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
