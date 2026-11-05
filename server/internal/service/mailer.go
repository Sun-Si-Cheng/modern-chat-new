// Package service 业务服务：邮件发送等。
package service

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
)

// mailTimeout 单个连接的建连/读写上限，避免 SMTP 故障拖住请求。
const mailTimeout = 10 * time.Second

// Mailer 找回密码邮件：未配置 SMTP 时仅打日志（开发期约定）。
//
// 配置可在运行时被后台修改（系统设置页），因此读写都要加锁：
// SendResetLink 可能正被请求协程调用，而另一个协程在保存新配置。
type Mailer struct {
	mu  sync.RWMutex
	cfg config.SMTPConfig
}

func NewMailer(cfg config.SMTPConfig) *Mailer { return &Mailer{cfg: cfg} }

// SetConfig 热更新配置（后台保存系统设置后调用）。
func (m *Mailer) SetConfig(cfg config.SMTPConfig) {
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
}

// Config 返回当前配置快照（用于后台回显与连通性测试）。
func (m *Mailer) Config() config.SMTPConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// SendResetLink 发送重置链接；失败只记日志，不影响响应（不泄露邮箱是否存在）。
//
// 兼容两种常见服务商形态：
//   - 465：隐式 TLS（QQ / 163 等），连接即加密
//   - 587 / 25：明文连接后 STARTTLS 升级，再认证
func (m *Mailer) SendResetLink(to, link, token string) {
	cfg := m.Config()
	if !cfg.Enabled() {
		// 日志里出现完整链接 = 把可用的重置令牌暴露给所有能读日志的人
		//（日志平台、运维脚本、备份）。生产默认只记「未配置」，需要排查时
		// 显式设置 LOG_RESET_LINK=true。
		if cfg.LogResetLink {
			slog.Warn("mail(SMTP 未配置) password reset link", "to", to, "link", link)
		} else {
			slog.Warn("mail(SMTP 未配置) 重置邮件未发送；如需在日志中查看链接请设置 LOG_RESET_LINK=true",
				"to", to)
		}
		return
	}
	if err := sendMail(cfg, to, buildResetMessage(cfg, to, link, token)); err != nil {
		slog.Error("mail send failed", "to", to, "host", cfg.Host, "port", cfg.Port, "err", err.Error())
		return
	}
	slog.Info("mail sent", "to", to)
}

// SendVerifyCode 发送注册验证码。
//
// 与重置链接不同，这里**返回错误**：注册接口必须能明确告诉用户
// 「邮件没发出去」，否则用户会一直等一封永远不来的邮件。
func (m *Mailer) SendVerifyCode(to, code string, ttl time.Duration) error {
	cfg := m.Config()
	if !cfg.Enabled() {
		slog.Warn("mail(SMTP 未配置) 验证码未发送", "to", to)
		return errors.New("SMTP 未配置")
	}
	body := "你的注册验证码是：" + code + "\r\n\r\n" +
		fmt.Sprintf("有效期 %d 分钟，仅可使用一次，请勿转发给他人。\r\n\r\n", int(ttl.Minutes())) +
		"若非本人操作，请忽略本邮件。\r\n"
	if err := sendMail(cfg, to, buildPlainMessage(cfg, to, "ModernChat 注册验证码", body)); err != nil {
		slog.Error("mail send failed", "to", to, "host", cfg.Host, "port", cfg.Port, "err", err.Error())
		return err
	}
	slog.Info("mail sent", "to", to, "kind", "verify-code")
	return nil
}

// SendTest 给指定地址发一封测试邮件，返回具体错误供后台展示。
// 与找回密码不同：这里必须把失败原因暴露给站长，否则配错了也看不出。
func (m *Mailer) SendTest(to string) error {
	cfg := m.Config()
	if !cfg.Enabled() {
		return errors.New("SMTP 尚未配置（host / from 为空）")
	}
	msg := buildPlainMessage(cfg, to, "ModernChat SMTP 测试",
		"这是一封来自 ModernChat 后台的测试邮件。\r\n\r\n收到它说明 SMTP 配置可用。\r\n")
	return sendMail(cfg, to, msg)
}

// buildResetMessage 组装 MIME 邮件：主题 RFC 2047 编码、正文 base64，避免中文乱码。
//
// 正文同时给出「链接」与「令牌原文」：邮件客户端把链接转成不可点、或被安全网关
// 改写时，用户仍能把令牌粘到找回密码页面，不至于整条流程断死。
func buildResetMessage(cfg config.SMTPConfig, to, link, token string) []byte {
	body := "请在 15 分钟内点击以下链接重置密码：\r\n\r\n" + link +
		"\r\n\r\n若链接无法点击，可打开「忘记密码」页面并把下面的令牌粘贴进去：\r\n\r\n" + token +
		"\r\n\r\n若非本人操作，请忽略本邮件。\r\n"
	return buildPlainMessage(cfg, to, "ModernChat 密码重置", body)
}

// buildPlainMessage 通用纯文本邮件组装。
func buildPlainMessage(cfg config.SMTPConfig, to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: ModernChat <" + cfg.From + ">\r\n")
	b.WriteString("To: <" + to + ">\r\n")
	b.WriteString("Subject: " + mime.BEncoding.Encode("UTF-8", subject) + "\r\n")
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

func sendMail(cfg config.SMTPConfig, to string, msg []byte) error {
	host := cfg.Host
	addr := net.JoinHostPort(host, cfg.Port)
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: mailTimeout}

	var conn net.Conn
	var err error
	if cfg.Port == "465" {
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

	if cfg.Port != "465" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		} else if cfg.User != "" {
			// 明文连接上不允许发送凭据（smtp.PlainAuth 也会拒绝），直接报错更清晰
			return fmt.Errorf("server does not support STARTTLS; use port 465 for implicit TLS")
		}
	}

	if cfg.User != "" {
		auth := smtp.PlainAuth("", cfg.User, cfg.Pass, host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := c.Mail(cfg.From); err != nil {
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
