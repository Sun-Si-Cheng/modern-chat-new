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
