package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// 上传限制。
const (
	MaxImageBytes = 8 << 20 // 8 MB：手机直出照片通常在 3-5 MB
	MaxAudioBytes = 5 << 20 // 5 MB：1 分钟语音约 0.5-1 MB
	// 表情图会被频繁加载，限制比普通图片更严：500KB 足够一张透明 PNG/GIF
	MaxStickerBytes = 2 << 20
	// 群文件：文档/压缩包较大，但仍要设上限避免被当网盘
	MaxFileBytes = 20 << 20
	// 视频：手机 1 分钟 1080p 约 20-40 MB，默认给 30 MB（后台可调 1-100 MB）
	MaxVideoBytes = 30 << 20

	// MaxAllowedUploadBytes 后台可配置的上限天花板（200 MB）。
	// 请求体中间件按它放宽，真正的逐类型限额由 handler 在读之前卡住。
	MaxAllowedUploadBytes = 200 << 20
)

// fileTypes 群文件允许的类型（白名单，MIME → 扩展名）。
//
// 刻意不含 text/html 与 image/svg+xml：这两类在浏览器里会被当页面渲染，
// 用户点开就等于在同源下执行上传者写的脚本（存储型 XSS）。
// docx/xlsx/pptx 是 zip 容器，DetectContentType 会判成 application/zip。
var fileTypes = map[string]string{
	"application/pdf":  ".pdf",
	"application/zip":  ".zip",
	"text/plain":       ".txt",
	"application/json": ".json",
	"image/jpeg":       ".jpg",
	"image/png":        ".png",
	"image/webp":       ".webp",
	"image/gif":        ".gif",
}

// 允许的类型（MIME → 扩展名）。
//
// **必须靠文件头判断真实类型**：只看 Content-Type 或扩展名的话，
// 把 .html 改名成 .png 就能上传，配合浏览器 MIME 嗅探可能变成存储型 XSS。
var imageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

var audioTypes = map[string]string{
	"audio/webm":  ".webm",
	"audio/ogg":   ".ogg",
	"audio/mpeg":  ".mp3",
	"audio/mp4":   ".m4a",
	"audio/wave":  ".wav",
	"audio/x-wav": ".wav",
}

// videoTypes 允许的视频容器（MIME → 扩展名）。
//
// 只收浏览器能直接播放的容器：mp4(H.264) / webm / mov / m4v。
// 刻意不含 avi、wmv、flv：即使上传成功，接收方点开也只是一片黑，
// 不如直接拒绝并提示转成 mp4。
var videoTypes = map[string]string{
	"video/mp4":       ".mp4",
	"video/webm":      ".webm",
	"video/quicktime": ".mov",
	"video/x-m4v":     ".m4v",
}

// Storage 文件存储抽象。本地磁盘与对象存储都实现它。
//
// 之所以要接口：消息里存的就是一个 URL，换存储实现时数据不用动。
type Storage interface {
	// Save 校验并保存，返回可访问的 URL
	Save(kind string, data []byte, maxBytes int64) (string, error)
	// Owns 判断 URL 是否属于本存储（消息内容校验用，防外链）
	Owns(url string) bool
	// Kind 实现名，用于日志与后台展示
	Kind() string
}

// FileStreamSaver 流式保存能力：上传 handler 优先用它，文件不整块进内存。
// 做成可选接口而不是并入 Storage，是为了不破坏测试里的最小 fake 实现。
type FileStreamSaver interface {
	SaveStream(kind string, r io.Reader, maxBytes int64) (string, error)
}

// StorageConfig 存储配置：Kind=local 忽略后面几项；Kind=s3 时启用对象存储。
type StorageConfig struct {
	Kind       string
	DataDir    string // local 用
	Endpoint   string // s3：如 minio.example.com:9000 或 s3.amazonaws.com
	Region     string
	Bucket     string
	AccessKey  string
	SecretKey  string
	PublicBase string // s3：对外访问前缀（CDN 域名），留空则按 endpoint/bucket 拼
}

// NewStorage 按配置构造存储实现。
//
// 配置了 S3 就**不会**在失败时静默回退本地：站长既然填了对象存储，
// 数据却偷偷写到本机磁盘，会造成"以为上传到云端、重装机器全丢"的事故。
func NewStorage(cfg StorageConfig) (Storage, error) {
	if cfg.Kind == "s3" && cfg.Endpoint != "" && cfg.Bucket != "" {
		s, err := newS3Storage(cfg)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	return NewLocalStorage(cfg.DataDir)
}

// ---------- 本地磁盘 ----------

// LocalStorage 本地磁盘存储。
type LocalStorage struct {
	root    string
	baseURL string
}

// UploadsDir 本地上传目录。
//
// 不管当前用哪种存储，历史文件都在这个目录里：切换到对象存储后，
// 老的 /uploads/... 消息仍要能打开，static 服务也仍然挂在这里。
func UploadsDir(dataDir string) string { return filepath.Join(dataDir, "uploads") }

func NewLocalStorage(dataDir string) (*LocalStorage, error) {
	root := UploadsDir(dataDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("创建上传目录失败: %w", err)
	}
	return &LocalStorage{root: root, baseURL: "/uploads"}, nil
}

func (s *LocalStorage) Kind() string { return "local" }

// Root 上传根目录（供静态服务使用）。
func (s *LocalStorage) Root() string { return s.root }

func (s *LocalStorage) Owns(url string) bool {
	return strings.HasPrefix(url, s.baseURL+"/") && !strings.Contains(url, "..")
}

func (s *LocalStorage) Save(kind string, data []byte, maxBytes int64) (string, error) {
	ext, err := detectType(kind, data, maxBytes)
	if err != nil {
		return "", err
	}
	rel := path.Join(time.Now().Format("2006"), time.Now().Format("01"))
	dir := filepath.Join(s.root, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := randomName() + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	return path.Join(s.baseURL, rel, name), nil
}

// SaveStream 流式保存：文件不整块进内存，边读边写临时文件，拷完再原子改名。
//
// 为什么要有它：视频上限可调到 100 MB，整块 `io.ReadAll` 意味着每个并发上传
// 都在内存里常驻一份文件本体（multipart 解析还可能先落一份临时文件），
// 几个并发就能把小实例打爆。嗅探只需要文件头 512 字节。
func (s *LocalStorage) SaveStream(kind string, r io.Reader, maxBytes int64) (string, error) {
	head := make([]byte, 512)
	hn, err := io.ReadFull(r, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	head = head[:hn]
	if hn == 0 {
		return "", fmt.Errorf("文件为空")
	}
	mime := sniffMime(head)
	ext, ok := extFor(kind, mime, head)
	if !ok {
		return "", fmt.Errorf("不支持的文件格式（检测为 %s）", mime)
	}

	rel := path.Join(time.Now().Format("2006"), time.Now().Format("01"))
	dir := filepath.Join(s.root, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	// LimitReader 多留 1 字节：写满说明超限，而不是恰好卡线。
	// 头部 512 字节已从 r 读走，也要计入总量，否则上限会松动 512B。
	remain := maxBytes + 1 - int64(hn)
	n, err := io.Copy(tmp, io.LimitReader(r, remain))
	if err != nil {
		cleanup()
		return "", err
	}
	total := int64(hn) + n
	if total == 0 {
		cleanup()
		return "", fmt.Errorf("文件为空")
	}
	if total > maxBytes {
		cleanup()
		return "", fmt.Errorf("文件超过 %d MB", maxBytes>>20)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	name := randomName() + ext
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return path.Join(s.baseURL, rel, name), nil
}

// ---------- S3 兼容对象存储 ----------

// S3Storage S3 兼容存储（AWS S3 / MinIO / 阿里云 OSS / 腾讯云 COS）。
type S3Storage struct {
	client     *minio.Client
	bucket     string
	publicBase string
}

func newS3Storage(cfg StorageConfig) (*S3Storage, error) {
	endpoint := strings.TrimSuffix(cfg.Endpoint, "/")
	secure := true
	if strings.HasPrefix(endpoint, "http://") {
		secure = false
		endpoint = strings.TrimPrefix(endpoint, "http://")
	} else {
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化对象存储失败: %w", err)
	}

	base := strings.TrimSuffix(cfg.PublicBase, "/")
	if base == "" {
		scheme := "https"
		if !secure {
			scheme = "http"
		}
		base = fmt.Sprintf("%s://%s/%s", scheme, endpoint, cfg.Bucket)
	}

	return &S3Storage{client: client, bucket: cfg.Bucket, publicBase: base}, nil
}

func (s *S3Storage) Kind() string { return "s3" }

func (s *S3Storage) Owns(url string) bool {
	if strings.Contains(url, "..") {
		return false
	}
	// 也接受历史本地地址：切到对象存储之前上传的图片/语音仍挂在本地，
	// 不认它们会导致「历史图片无法转发/引用」这种莫名退化。
	return strings.HasPrefix(url, s.publicBase+"/") || strings.HasPrefix(url, "/uploads/")
}

func (s *S3Storage) Save(kind string, data []byte, maxBytes int64) (string, error) {
	ext, err := detectType(kind, data, maxBytes)
	if err != nil {
		return "", err
	}
	key := path.Join(time.Now().Format("2006"), time.Now().Format("01"), randomName()+ext)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err = s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: mimeOfExt(kind, ext)})
	if err != nil {
		// 明确报错而不是回退本地：否则会出现"配置了云端、文件却在本机"的隐性数据分裂
		return "", fmt.Errorf("上传到对象存储失败: %w", err)
	}
	return s.publicBase + "/" + key, nil
}

// SaveStream 流式保存到对象存储：先落一个本地临时文件（限制大小、校验类型），
// 再以已知长度 PutObject——直接把未知长度的 reader 交给 S3 会退化成分片上传
// 且无法在超限时中止。
func (s *S3Storage) SaveStream(kind string, r io.Reader, maxBytes int64) (string, error) {
	head := make([]byte, 512)
	hn, err := io.ReadFull(r, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	head = head[:hn]
	if hn == 0 {
		return "", fmt.Errorf("文件为空")
	}
	mime := sniffMime(head)
	ext, ok := extFor(kind, mime, head)
	if !ok {
		return "", fmt.Errorf("不支持的文件格式（检测为 %s）", mime)
	}

	tmp, err := os.CreateTemp("", "mc-upload-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	// 与本地实现一致：头部计入总量，LimitReader 留 1 字节余量判超限
	remain := maxBytes + 1 - int64(hn)
	n, err := io.Copy(tmp, io.LimitReader(r, remain))
	if err != nil {
		cleanup()
		return "", err
	}
	total := int64(hn) + n
	if total == 0 {
		cleanup()
		return "", fmt.Errorf("文件为空")
	}
	if total > maxBytes {
		cleanup()
		return "", fmt.Errorf("文件超过 %d MB", maxBytes>>20)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}

	key := path.Join(time.Now().Format("2006"), time.Now().Format("01"), randomName()+ext)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	f, err := os.Open(tmpName)
	if err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmpName)
	}()

	_, err = s.client.PutObject(ctx, s.bucket, key, f, n,
		minio.PutObjectOptions{ContentType: mimeOfExt(kind, ext)})
	if err != nil {
		return "", fmt.Errorf("上传到对象存储失败: %w", err)
	}
	return s.publicBase + "/" + key, nil
}

// DirUsage 统计目录内文件数与总字节数（配额检查与后台展示用）。
func DirUsage(root string) (files int, bytes int64, err error) {
	err = filepath.Walk(root, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		files++
		bytes += info.Size()
		return nil
	})
	return files, bytes, err
}

// sniffMime 剥掉 charset 参数后的 MIME（http.DetectContentType 对文本会带参数）。
func sniffMime(head []byte) string {
	mime := http.DetectContentType(head)
	if i := strings.IndexByte(mime, ';'); i > 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	return mime
}

// ---- 本地存储配额（带短 TTL 缓存） ----
//
// 旧实现在每次上传时 filepath.Walk 整个上传目录：O(文件数) 次 stat 系统调用，
// 5 万个历史文件就是 5 万+ 次 syscall，且与正在写盘的请求抢 IO。
// 这里缓存 60 秒：上传成功后增量累加，过期或换根时重扫一次。
// 管理员清理孤儿文件后最多 1 分钟才反映到配额判断，可接受。

var quotaCache struct {
	sync.Mutex
	root    string
	used    int64
	scanned time.Time
}

const quotaCacheTTL = time.Minute

// CachedDirUsage 返回本地存储已用字节数（优先走缓存）。
func CachedDirUsage(root string) (int64, error) {
	quotaCache.Lock()
	if quotaCache.root == root && time.Since(quotaCache.scanned) < quotaCacheTTL {
		used := quotaCache.used
		quotaCache.Unlock()
		return used, nil
	}
	quotaCache.Unlock()

	_, used, err := DirUsage(root)
	if err != nil {
		return 0, err
	}
	quotaCache.Lock()
	quotaCache.root, quotaCache.used, quotaCache.scanned = root, used, time.Now()
	quotaCache.Unlock()
	return used, nil
}

// AddQuotaDelta 上传成功后累加缓存用量，避免下一次上传又触发全目录扫描。
func AddQuotaDelta(root string, delta int64) {
	quotaCache.Lock()
	if quotaCache.root == root {
		quotaCache.used += delta
	}
	quotaCache.Unlock()
}

// ---------- 共用 ----------

// extFor 按 kind + 嗅探到的 MIME + 容器特征返回扩展名。
// 从 detectType 拆出来供流式保存复用（流式时手里只有文件头，大小检查在拷贝后做）。
func extFor(kind, mime string, data []byte) (string, bool) {
	var allowed map[string]string
	switch kind {
	case "image":
		allowed = imageTypes
	case "audio":
		allowed = audioTypes
	case "sticker":
		allowed = imageTypes
	case "file":
		allowed = fileTypes
	case "video":
		allowed = videoTypes
	default:
		return "", false
	}
	if ext, ok := allowed[mime]; ok {
		return ext, true
	}
	// DetectContentType 把 WebM 容器统一判为 video/webm（浏览器 MediaRecorder 录的
	// 音频也是这个容器），Ogg 判为 application/ogg，纯音频 mp4 也可能判成 video/mp4。
	// 语音上传已限定 kind=audio 且 ≤5MB，这里按容器特征兜底识别为音频扩展名。
	if kind == "audio" && (strings.HasPrefix(mime, "video/") || mime == "application/ogg") {
		if ext := guessAudioExt(data); ext != "" {
			return ext, true
		}
	}
	// DetectContentType 对 webm / m4a 常返回 application/octet-stream
	if kind == "audio" && strings.HasPrefix(mime, "application/octet-stream") {
		if ext := guessAudioExt(data); ext != "" {
			return ext, true
		}
	}
	// 视频同理，且更严重：Go 的嗅探表只认 `\x00\x00\x00\x18ftypmp42` 等少数 brand，
	// 真实手机/相机导出的 mp4（brand 是 isom / avc1 / mp42 以外）会判成
	// application/octet-stream，不兜底就一律「不支持的文件格式」。
	if kind == "video" {
		if ext := guessVideoExt(data); ext != "" {
			return ext, true
		}
	}
	return "", false
}

// detectType 按文件头判断真实类型并返回扩展名（整文件在内存时的路径，测试与旧调用用）。
func detectType(kind string, data []byte, maxOverride int64) (string, error) {
	var maxBytes int
	switch kind {
	case "image":
		maxBytes = MaxImageBytes
	case "audio":
		maxBytes = MaxAudioBytes
	case "sticker":
		maxBytes = MaxStickerBytes
	case "file":
		maxBytes = MaxFileBytes
	case "video":
		maxBytes = MaxVideoBytes
	default:
		return "", fmt.Errorf("不支持的文件类型")
	}
	// 调用方（上传接口）按后台配置传上限；0 表示用内置默认值。
	// 这样「体积上限」是业务策略，不必写死在存储层。
	if maxOverride > 0 {
		maxBytes = int(maxOverride)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("文件为空")
	}
	if len(data) > maxBytes {
		return "", fmt.Errorf("文件超过 %d MB", maxBytes>>20)
	}

	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	mime := http.DetectContentType(sniff)
	// DetectContentType 对文本会返回 "text/plain; charset=utf-8"，
	// 直接拿去查白名单永远匹配不上，必须先剥掉参数
	if i := strings.IndexByte(mime, ';'); i > 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	if ext, ok := extFor(kind, mime, data); ok {
		return ext, nil
	}
	return "", fmt.Errorf("不支持的文件格式（检测为 %s）", mime)
}

// guessAudioExt 按容器特征判断音频扩展名。
func guessAudioExt(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	if data[0] == 0x1A && data[1] == 0x45 && data[2] == 0xDF && data[3] == 0xA3 {
		return ".webm" // WebM / Matroska
	}
	if string(data[:4]) == "OggS" {
		return ".ogg"
	}
	if string(data[4:8]) == "ftyp" {
		return ".m4a"
	}
	if string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return ".wav"
	}
	if string(data[:3]) == "ID3" {
		return ".mp3"
	}
	return ""
}

// mimeOfExt 按 kind + 扩展名给出对象存储的 Content-Type。
//
// 必须带 kind：`.webm` 既可能是语音（MediaRecorder 录音）也可能是视频，
// 只看扩展名会把视频标成 audio/webm，浏览器拿到后按音频处理、画面出不来。
// guessVideoExt 按容器特征判断视频扩展名。
//
// ISO-BMFF（mp4/mov/m4v）统一以 "ftyp" box 开头，靠 brand 区分具体容器；
// 纯音频的 m4a 也用 ftyp（brand 为 M4A /M4B），这里明确拒掉，
// 避免把音频当视频存成 .mp4（视频元素能出声但画面永远黑）。
func guessVideoExt(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	if data[0] == 0x1A && data[1] == 0x45 && data[2] == 0xDF && data[3] == 0xA3 {
		return ".webm" // WebM / Matroska
	}
	// ftyp 一般紧跟 4 字节 box size，但个别工具会先塞 free/skip box，
	// 所以在前 512 字节里找而不是死抠偏移
	limit := len(data)
	if limit > 512 {
		limit = 512
	}
	idx := bytes.Index(data[:limit], []byte("ftyp"))
	if idx < 0 || idx+8 > len(data) {
		return ""
	}
	switch brand := string(data[idx+4 : idx+8]); brand {
	case "qt  ":
		return ".mov"
	case "M4V ", "M4VH", "M4VP":
		return ".m4v"
	case "M4A ", "M4B ", "M4P ":
		return "" // 纯音频容器：不接受
	default:
		return ".mp4"
	}
}

func mimeOfExt(kind, ext string) string {
	switch ext {
	case ".jpg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".m4v":
		return "video/x-m4v"
	case ".webm":
		if kind == "video" {
			return "video/webm"
		}
		return "audio/webm"
	case ".ogg":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	}
	return "application/octet-stream"
}

func randomName() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
