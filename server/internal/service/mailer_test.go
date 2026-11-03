package service

import (
	"encoding/base64"
	"strings"
	"testing"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
)

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]string{
		"  User@Example.COM ": "user@example.com",
		"a@b.com":             "a@b.com",
		"":                    "",
	}
	for in, want := range cases {
		if got := NormalizeEmail(in); got != want {
			t.Fatalf("NormalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildResetMessage(t *testing.T) {
	m := NewMailer(config.SMTPConfig{Host: "smtp.qq.com", Port: "465", From: "no-reply@example.com"})
	link := "https://chat.example.com/reset?token=abc123"
	msg := string(buildResetMessage(m.Config(), "user@example.com", link))

	// 必备头字段
	for _, want := range []string{
		"From: ModernChat <no-reply@example.com>",
		"To: <user@example.com>",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: base64",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("缺少头字段 %q：\n%s", want, msg)
		}
	}

	// 主题含中文，必须按 RFC 2047 编码（Go 的 BEncoding 输出小写 b，标记大小写不敏感）
	subjLower := strings.ToLower(msg)
	if !strings.Contains(subjLower, "subject: =?utf-8?") {
		t.Fatalf("主题未按 RFC 2047 编码：\n%s", msg)
	}

	// 正文：base64 解码后应包含重置链接
	sep := strings.Index(msg, "\r\n\r\n")
	if sep < 0 {
		t.Fatalf("缺少头部与正文的分隔空行：\n%s", msg)
	}
	body := strings.TrimSpace(msg[sep+4:])
	// RFC 2045：base64 每行不超过 76 字符
	for _, line := range strings.Split(body, "\r\n") {
		if len(line) > 76 {
			t.Fatalf("base64 行长度为 %d，超过 76：%s", len(line), line)
		}
	}
	dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body, "\r\n", ""))
	if err != nil {
		t.Fatalf("正文不是合法 base64: %v", err)
	}
	if !strings.Contains(string(dec), link) {
		t.Fatalf("解码后的正文应包含重置链接，got %q", dec)
	}
}

// TestSendResetLinkWithoutSMTP 未配置 SMTP 时只打日志：不得 panic、不得外发。
func TestSendResetLinkWithoutSMTP(t *testing.T) {
	m := NewMailer(config.SMTPConfig{})
	m.SendResetLink("user@example.com", "https://chat.example.com/reset?token=t")
}

// TestSendResetLinkDialFailure 服务端不可达时失败只记日志，不 panic、不影响调用方。
func TestSendResetLinkDialFailure(t *testing.T) {
	m := NewMailer(config.SMTPConfig{
		Host: "127.0.0.1", Port: "1", User: "u", Pass: "p", From: "no-reply@example.com",
	})
	m.SendResetLink("user@example.com", "https://chat.example.com/reset?token=t")
}
