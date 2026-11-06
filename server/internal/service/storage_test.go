package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// 浏览器录音的容器常被 http.DetectContentType 判成 video/* 或 application/ogg，
// 语音上传必须仍能识别出音频扩展名，否则用户发语音会被直接拒掉。
func TestDetectTypeAcceptsBrowserAudioContainers(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		// Chrome / Edge MediaRecorder 默认 audio/webm（WebM 容器 -> video/webm）
		{"webm", []byte("\x1a\x45\xdf\xa3webm-payload"), ".webm"},
		// Firefox MediaRecorder 默认 audio/ogg（Ogg 容器 -> application/ogg）
		{"ogg", []byte("OggS\x00\x02payload"), ".ogg"},
		// Safari 的 audio/mp4（部分嗅探结果会是 video/mp4）
		{"mp4", []byte("\x00\x00\x00\x18ftypM4A \x00\x00\x00\x00"), ".m4a"},
	}
	for _, c := range cases {
		ext, err := detectType("audio", c.data, 0)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if ext != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, ext, c.want)
		}
	}
}

// http.DetectContentType 对文本会返回 "text/plain; charset=utf-8"，
// 不剥掉参数就永远匹配不上白名单 —— 曾导致上传 txt/docx 一律被拒。
func TestDetectTypeStripsMimeParameters(t *testing.T) {
	ext, err := detectType("file", []byte("hello, plain text payload"), 0)
	if err != nil {
		t.Fatalf("text file rejected: %v", err)
	}
	if ext != ".txt" {
		t.Fatalf("got %q, want .txt", ext)
	}
}

// 群文件的真正安全保证：**落盘扩展名由嗅探结果决定**，不是用户给的文件名。
// HTML 直接拒；SVG 会被判成 text/plain → 存成 .txt → 服务时是 text/plain，
// 浏览器不会当页面渲染，因此不构成存储型 XSS。
func TestDetectTypeNeverYieldsExecutableMarkupExtension(t *testing.T) {
	if _, err := detectType("file", []byte("<!doctype html><script>alert(1)</script>"), 0); err == nil {
		t.Fatal("html should be rejected")
	}
	ext, err := detectType("file", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), 0)
	if err != nil {
		return // 拒绝也可以
	}
	if ext != ".txt" {
		t.Fatalf("svg stored as %q, want .txt or rejection", ext)
	}
}

// 视频按容器白名单识别：mp4 / webm / mov 通过，其他容器拒绝（浏览器播不了）。
func TestDetectTypeVideo(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		// MP4：第 4-8 字节是 ftyp box
		{"mp4", []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00"), ".mp4"},
		// WebM 容器（Magic 0x1A45DFA3）
		{"webm", []byte("\x1a\x45\xdf\xa3video-payload"), ".webm"},
		// QuickTime：ftypqt
		{"mov", []byte("\x00\x00\x00\x14ftypqt  \x00\x00\x00\x00"), ".mov"},
	}
	for _, c := range cases {
		ext, err := detectType("video", c.data, 0)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if ext != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, ext, c.want)
		}
	}
	// 非视频内容不能借 kind=video 混进来
	if _, err := detectType("video", []byte("hello, plain text payload"), 0); err == nil {
		t.Fatal("plain text must be rejected for kind=video")
	}
}

// 视频超过上限要拒绝（上限由后台配置传入，0 表示用内置默认值）。
func TestDetectTypeVideoSizeLimit(t *testing.T) {
	data := []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00")
	if _, err := detectType("video", data, 4); err == nil {
		t.Fatal("oversized video should be rejected")
	}
}

// `.webm` 既可能是语音也可能是视频：对象存储的 Content-Type 必须按 kind 区分，
// 否则视频会被标成 audio/webm，浏览器拿到后按音频处理、画面出不来。
func TestMimeOfExtDistinguishesWebm(t *testing.T) {
	if got := mimeOfExt("video", ".webm"); got != "video/webm" {
		t.Fatalf("video webm: got %q", got)
	}
	if got := mimeOfExt("audio", ".webm"); got != "audio/webm" {
		t.Fatalf("audio webm: got %q", got)
	}
	if got := mimeOfExt("video", ".mp4"); got != "video/mp4" {
		t.Fatalf("mp4: got %q", got)
	}
}

// 流式保存：类型嗅探、大小上限、空文件与配额增量都要成立。
// 这是上传接口的主路径（文件不整块进内存），坏了影响所有媒体上传。
func TestSaveStreamLocal(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatal(err)
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte("x"), 100)...)
	url, err := s.SaveStream("image", bytes.NewReader(png), MaxImageBytes)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.HasSuffix(url, ".png") || !strings.HasPrefix(url, "/uploads/") {
		t.Fatalf("unexpected url %q", url)
	}

	// 超限：maxBytes=4，png 头就有 8 字节
	if _, err := s.SaveStream("image", bytes.NewReader(png), 4); err == nil {
		t.Fatal("oversize stream should be rejected")
	}

	// 空文件
	if _, err := s.SaveStream("image", bytes.NewReader(nil), 8); err == nil {
		t.Fatal("empty stream should be rejected")
	}

	// 伪装成 png 的 html：嗅探按文件头判，必须拒绝
	html := []byte("<!doctype html><script>alert(1)</script>")
	if _, err := s.SaveStream("image", bytes.NewReader(html), 1<<20); err == nil {
		t.Fatal("html disguised as image should be rejected")
	}

	// 落盘的文件确实存在，且配额缓存能通过 DirUsage 查到它
	rel := strings.TrimPrefix(url, "/uploads/")
	if _, err := os.Stat(filepath.Join(dir, "uploads", filepath.FromSlash(rel))); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
	if _, err := CachedDirUsage(filepath.Join(dir, "uploads")); err != nil {
		t.Fatal(err)
	}
	before, _ := CachedDirUsage(filepath.Join(dir, "uploads"))
	AddQuotaDelta(filepath.Join(dir, "uploads"), 500)
	after, _ := CachedDirUsage(filepath.Join(dir, "uploads"))
	if after != before+500 {
		t.Fatalf("quota delta not applied: %d -> %d", before, after)
	}
}

// 切到对象存储后，历史消息里的 /uploads/... 仍必须被认作「本站文件」，
// 否则老图片/语音无法转发、引用（功能静默退化）。
func TestS3OwnsAcceptsLegacyLocalUploads(t *testing.T) {
	s := &S3Storage{publicBase: "https://cdn.example.com"}
	cases := []struct {
		url  string
		want bool
	}{
		{"/uploads/2026/11/abc.png", true},
		{"https://cdn.example.com/2026/11/abc.png", true},
		{"https://evil.example/x.png", false},
		{"/uploads/../../etc/passwd", false},
	}
	for _, c := range cases {
		if got := s.Owns(c.url); got != c.want {
			t.Fatalf("%s: got %v, want %v", c.url, got, c.want)
		}
	}
}

// roundTripImagePayload 构造头尾都不同的图片内容。
// 前 512 字节会被 SaveStream 读去做类型嗅探，若忘记写回就会丢文件头。
func roundTripImagePayload() []byte {
	b := make([]byte, 1024)
	copy(b, "\x89PNG\r\n\x1a\n")
	for i := 8; i < len(b); i++ {
		b[i] = byte(i)
	}
	return b
}

// roundTripTextPayload 纯文本内容：不触发缩略图，便于单独验证 S3 上传字节数。
func roundTripTextPayload() []byte {
	b := make([]byte, 1024)
	n := copy(b, "this is a plain text payload\n")
	for i := n; i < len(b); i++ {
		b[i] = byte('a' + i%26)
	}
	return b
}

// 回归：上传内容必须逐字节原样落盘。
// 旧实现只拷贝嗅探后的剩余部分，前 512 字节被静默丢弃，图片/视频全部损坏。
func TestSaveStreamLocalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLocalStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	payload := roundTripImagePayload()
	url, err := s.SaveStream("image", bytes.NewReader(payload), MaxImageBytes)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	rel := strings.TrimPrefix(url, "/uploads/")
	body, err := os.ReadFile(filepath.Join(dir, "uploads", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("round-trip mismatch: saved %d bytes, uploaded %d bytes", len(body), len(payload))
	}
}

// S3 分支同样必须逐字节完整：用 httptest 伪 S3 接收 PutObject，
// 覆盖「头部写回 + 上传长度含头」两点（不连接真实对象存储）。
func TestSaveStreamS3RoundTrip(t *testing.T) {
	var mu sync.Mutex
	var puts [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read put body: %v", err)
			}
			mu.Lock()
			puts = append(puts, body)
			mu.Unlock()
			w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s, err := newS3Storage(StorageConfig{
		Kind:      "s3",
		Endpoint:  srv.URL,
		Region:    "us-east-1",
		Bucket:    "bucket",
		AccessKey: "ak",
		SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("new s3: %v", err)
	}
	payload := roundTripTextPayload()
	if _, err := s.SaveStream("file", bytes.NewReader(payload), MaxFileBytes); err != nil {
		t.Fatalf("save stream: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(puts) != 1 {
		t.Fatalf("expected 1 PUT, got %d", len(puts))
	}
	// minio-go 默认用 SigV4 streaming（aws-chunked）上传，body 带分块签名头；
	// 解出第一块载荷再逐字节比对，等价于服务端最终落盘的内容。
	decoded := decodeAWSChunked(t, puts[0])
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("round-trip mismatch: uploaded %d bytes, decoded %d bytes", len(payload), len(decoded))
	}
}

// decodeAWSChunked 从 aws-chunked 请求体里取出第一块（<hex-size>;chunk-signature=...\r\n + data）。
// 小文件只有一个数据块，取首块即可覆盖完整内容。
func decodeAWSChunked(t *testing.T, body []byte) []byte {
	t.Helper()
	i := bytes.Index(body, []byte("\r\n"))
	if i < 0 {
		t.Fatalf("malformed aws-chunked body: %q", string(body[:min(len(body), 64)]))
	}
	sizeStr := string(body[:i])
	if j := strings.IndexByte(sizeStr, ';'); j >= 0 {
		sizeStr = sizeStr[:j]
	}
	n, err := strconv.ParseInt(sizeStr, 16, 64)
	if err != nil {
		t.Fatalf("malformed chunk size %q: %v", sizeStr, err)
	}
	start := int64(i + 2)
	if int64(len(body)) < start+n {
		t.Fatalf("chunk truncated: need %d bytes from %d, body %d", n, start, len(body))
	}
	return body[start : start+n]
}
