// Package service 业务服务：邮件发送等。
package service

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
)

// mailTimeout 单个连接的建连/读写上限，避免 SMTP 故障拖住请求。
const mailTimeout = 10 * time.Second

// Mailer 找回密码邮件：未配置 SMTP 时仅打日志（开发期约定）。
type Mailer struct {
	cfg config.SMTPConfig
}

func NewMailer(cfg config.SMTPConfig) *Mailer { return &Mailer{cfg: cfg} }

// SendResetLink 发送重置链接；失败只记日志，不影响响应（不泄露邮箱是否存在）。
//
// 兼容两种常见服务商形态：
//   - 465：隐式 TLS（QQ / 163 等），连接即加密
//   - 587 / 25：明文连接后 STARTTLS 升级，再认证
func (m *Mailer) SendResetLink(to, link string) {
	if !m.cfg.Enabled() {
		slog.Info("mail(log-only) password reset link", "to", to, "link", link)
		return
	}
	if err := m.send(to, m.buildResetMessage(to, link)); err != nil {
		slog.Error("mail send failed", "to", to, "host", m.cfg.Host, "port", m.cfg.Port, "err", err.Error())
		return
	}
	slog.Info("mail sent", "to", to)
}

// buildResetMessage 组装 MIME 邮件：主题 RFC 2047 编码、正文 base64，避免中文乱码。
func (m *Mailer) buildResetMessage(to, link string) []byte {
	body := "请在 15 分钟内点击以下链接重置密码：\r\n\r\n" + link +
		"\r\n\r\n若非本人操作，请忽略本邮件。\r\n"

	var b strings.Builder
	b.WriteString("From: ModernChat <" + m.cfg.From + ">\r\n")
	b.WriteString("To: <" + to + ">\r\n")
	b.WriteString("Subject: " + mime.BEncoding.Encode("UTF-8", "ModernChat 密码重置") + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("\r\n")

	enc := base64.StdEncoding.EncodeToString([]byte(body))
	for len(enc) > 76 { // RFC 2045：每行不超过 76 字符
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return []byte(b.String())
}

func (m *Mailer) send(to string, msg []byte) error {
	host := m.cfg.Host
	addr := net.JoinHostPort(host, m.cfg.Port)
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: mailTimeout}

	var conn net.Conn
	var err error
	if m.cfg.Port == "465" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(mailTimeout))

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() { _ = c.Close() }()

	if m.cfg.Port != "465" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		} else if m.cfg.User != "" {
			// 明文连接上不允许发送凭据（smtp.PlainAuth 也会拒绝），直接报错更清晰
			return fmt.Errorf("server does not support STARTTLS; use port 465 for implicit TLS")
		}
	}

	if m.cfg.User != "" {
		auth := smtp.PlainAuth("", m.cfg.User, m.cfg.Pass, host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := c.Mail(m.cfg.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close body: %w", err)
	}
	return c.Quit()
}

// NormalizeEmail 去空格小写。
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
