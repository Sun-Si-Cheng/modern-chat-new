package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// 上传限制。
const (
	MaxImageBytes = 8 << 20 // 8 MB：手机直出照片通常在 3-5 MB
	MaxAudioBytes = 5 << 20 // 5 MB：1 分钟语音约 0.5-1 MB
)

// 允许的类型（扩展名 → 期望的 MIME 前缀）。
//
// **必须靠文件头判断真实类型**：只看 Content-Type 或扩展名的话，
// 把 .html 改名成 .png 就能上传，配合浏览器 MIME 嗅探可能变成存储型 XSS。
// 这也是为什么响应里始终带 X-Content-Type-Options: nosniff。
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

// Storage 本地文件存储。
//
// 之所以先做本地而不是直接接对象存储：项目当前是单机部署，
// 本地磁盘零依赖、零成本；接口按「返回一个 URL」设计，
// 以后换 S3 只需换实现，消息里的 content 不用动。
type Storage struct {
	root    string // 上传根目录（绝对路径）
	baseURL string // 对外访问前缀，如 /uploads
}

func NewStorage(dataDir string) (*Storage, error) {
	root := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("创建上传目录失败: %w", err)
	}
	return &Storage{root: root, baseURL: "/uploads"}, nil
}

// Root 上传根目录（供静态服务使用）。
func (s *Storage) Root() string { return s.root }

// Save 校验并保存一个文件，返回可访问的相对 URL。
//
// kind 只能是 image / audio；真实类型由文件头判定，与扩展名无关。
func (s *Storage) Save(kind string, data []byte) (string, error) {
	var allowed map[string]string
	var maxBytes int
	switch kind {
	case "image":
		allowed, maxBytes = imageTypes, MaxImageBytes
	case "audio":
		allowed, maxBytes = audioTypes, MaxAudioBytes
	default:
		return "", errors.New("不支持的文件类型")
	}

	if len(data) == 0 {
		return "", errors.New("文件为空")
	}
	if len(data) > maxBytes {
		return "", fmt.Errorf("文件超过 %d MB", maxBytes>>20)
	}

	// 用前 512 字节嗅探真实类型
	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	mime := http.DetectContentType(sniff)
	// DetectContentType 对 webm 常返回 application/octet-stream，单独兜底
	ext, ok := allowed[mime]
	if !ok {
		if kind == "audio" && strings.HasPrefix(mime, "application/octet-stream") {
			ext = guessAudioExt(data)
		}
		if ext == "" {
			return "", fmt.Errorf("不支持的文件格式（检测为 %s）", mime)
		}
	}

	// 按年月分目录，避免单目录文件过多
	now := time.Now()
	rel := path.Join(now.Format("2006"), now.Format("01"))
	dir := filepath.Join(s.root, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	name := randomName() + ext
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", err
	}

	return path.Join(s.baseURL, rel, name), nil
}

// guessAudioExt 按容器特征判断音频扩展名（DetectContentType 覆盖有限）。
func guessAudioExt(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	// WebM / Matroska：1A 45 DF A3
	if data[0] == 0x1A && data[1] == 0x45 && data[2] == 0xDF && data[3] == 0xA3 {
		return ".webm"
	}
	// OggS
	if string(data[:4]) == "OggS" {
		return ".ogg"
	}
	// ftyp（MP4 / M4A）
	if string(data[4:8]) == "ftyp" {
		return ".m4a"
	}
	// RIFF....WAVE
	if string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return ".wav"
	}
	// ID3（MP3）
	if string(data[:3]) == "ID3" {
		return ".mp3"
	}
	return ""
}

func randomName() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// rand 失败极少见；退回时间戳保证仍能命名
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// IsUploadURL 判断一个字符串是否是本站上传文件的相对地址。
//
// 消息落库前用它把 content 限死在自家上传目录：
// 否则用户可以构造 content = "https://evil.com/x.png"，
// 客户端会去加载外部图片（泄露 IP、可被用来做统计打点）。
func IsUploadURL(s string) bool {
	if !strings.HasPrefix(s, "/uploads/") {
		return false
	}
	// 禁止目录穿越
	if strings.Contains(s, "..") {
		return false
	}
	return len(s) <= 300
}
