package service

import "testing"

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
		ext, err := detectType("audio", c.data)
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
	ext, err := detectType("file", []byte("hello, plain text payload"))
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
	if _, err := detectType("file", []byte("<!doctype html><script>alert(1)</script>")); err == nil {
		t.Fatal("html should be rejected")
	}
	ext, err := detectType("file", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	if err != nil {
		return // 拒绝也可以
	}
	if ext != ".txt" {
		t.Fatalf("svg stored as %q, want .txt or rejection", ext)
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
