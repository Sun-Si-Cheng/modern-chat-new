package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
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
		minio.PutObjectOptions{ContentType: mimeOfExt(ext)})
	if err != nil {
		// 明确报错而不是回退本地：否则会出现"配置了云端、文件却在本机"的隐性数据分裂
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

// ---------- 共用 ----------

// detectType 按文件头判断真实类型并返回扩展名。
func detectType(kind string, data []byte, maxOverride int64) (string, error) {
	var allowed map[string]string
	var maxBytes int
	switch kind {
	case "image":
		allowed, maxBytes = imageTypes, MaxImageBytes
	case "audio":
		allowed, maxBytes = audioTypes, MaxAudioBytes
	case "sticker":
		// 表情走图片类型校验（同样按文件头判），只是体积上限更小
		allowed, maxBytes = imageTypes, MaxStickerBytes
	case "file":
		allowed, maxBytes = fileTypes, MaxFileBytes
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
	if ext, ok := allowed[mime]; ok {
		return ext, nil
	}
	// DetectContentType 把 WebM 容器统一判为 video/webm（浏览器 MediaRecorder 录的
	// 音频也是这个容器），Ogg 判为 application/ogg，纯音频 mp4 也可能判成 video/mp4。
	// 语音上传已限定 kind=audio 且 ≤5MB，这里按容器特征兜底识别为音频扩展名。
	if kind == "audio" && (strings.HasPrefix(mime, "video/") || mime == "application/ogg") {
		if ext := guessAudioExt(data); ext != "" {
			return ext, nil
		}
	}
	// DetectContentType 对 webm / m4a 常返回 application/octet-stream
	if kind == "audio" && strings.HasPrefix(mime, "application/octet-stream") {
		if ext := guessAudioExt(data); ext != "" {
			return ext, nil
		}
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

func mimeOfExt(ext string) string {
	switch ext {
	case ".jpg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".webm":
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
