// Package service 业务服务：发消息链路、presence、邮件。
package service

import (
	"log/slog"
	"net/smtp"
	"strings"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
)

// Mailer 找回密码邮件：未配置 SMTP 时仅打日志（开发期约定）。
type Mailer struct {
	cfg config.SMTPConfig
}

func NewMailer(cfg config.SMTPConfig) *Mailer { return &Mailer{cfg: cfg} }

// SendResetLink 发送重置链接；失败只记日志，不影响响应（不泄露邮箱是否存在）。
func (m *Mailer) SendResetLink(to, link string) {
	if !m.cfg.Enabled() {
		slog.Info("mail(log-only) 密码重置链接", "to", to, "link", link)
		return
	}
	addr := m.cfg.Host + ":" + m.cfg.Port
	from := m.cfg.From
	body := "From: ModernChat <" + from + ">\r\n" +
		"To: <" + to + ">\r\n" +
		"Subject: ModernChat 密码重置\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		"请在 15 分钟内点击以下链接重置密码：\r\n" + link + "\r\n\r\n" +
		"若非本人操作，请忽略本邮件。"
	err := smtp.SendMail(addr, nil, from, []string{to}, []byte(body))
	if err != nil {
		slog.Error("mail send failed", "to", to, "err", err.Error())
		return
	}
	slog.Info("mail sent", "to", to)
}

// NormalizeEmail 去空格小写。
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
