package handler

import (
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/service"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

// ---- 两步验证（2FA，默认关闭）----
//
// 支持两种第二因子（用户在设置页自行开启，可随时关闭）：
//   - 验证器应用（TOTP，RFC 6238，扫码 / 手动录入密钥）
//   - 邮箱验证码（复用 email_codes 存储，purpose=twofa）
// 另有一次性恢复码（启用时生成 8 个，单次有效，两种方式都可用）。
//
// 登录链路：密码正确后 Login 只发短寿命 2FA 票据（绑定 uid，5 分钟、最多试
// 5 次），POST /auth/2fa/verify 校验通过才签发 JWT。
//
// 状态存进程内（单实例口径，与 ssoTickets / qrSessions 一致）；恢复码哈希落库。

const (
	twofaTicketTTL      = 5 * time.Minute
	twofaTicketMaxTries = 5
	twofaEmailPurpose   = "twofa"
)

// ---- 2FA 票据（密码正确但第二因子未过的中间态） ----

type twofaTicketEntry struct {
	userID   uint
	expires  time.Time
	attempts int
}

var twofaTickets = struct {
	sync.Mutex
	m map[string]*twofaTicketEntry
}{m: map[string]*twofaTicketEntry{}}

func twofaIssueTicket(userID uint) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	twofaTickets.Lock()
	defer twofaTickets.Unlock()
	now := time.Now()
	for k, v := range twofaTickets.m {
		if now.After(v.expires) {
			delete(twofaTickets.m, k)
		}
	}
	twofaTickets.m[token] = &twofaTicketEntry{userID: userID, expires: now.Add(twofaTicketTTL)}
	return token, nil
}

// twofaTakeTicket 取票据（同时计一次尝试）：连续错 5 次作废票据，强制重新走密码登录。
func twofaTakeTicket(token string) (userID uint, alive bool) {
	twofaTickets.Lock()
	defer twofaTickets.Unlock()
	t, ok := twofaTickets.m[token]
	if !ok || time.Now().After(t.expires) || t.attempts >= twofaTicketMaxTries {
		if ok {
			delete(twofaTickets.m, token)
		}
		return 0, false
	}
	t.attempts++
	return t.userID, true
}

func twofaDropTicket(token string) {
	twofaTickets.Lock()
	defer twofaTickets.Unlock()
	delete(twofaTickets.m, token)
}

// ---- 恢复码归一化与校验（store 只存哈希） ----

// twofaRecoveryFormat 恢复码形态：去掉连字符后 8 位十六进制（大小写不敏感）。
func twofaRecoveryFormat(code string) bool {
	c := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(code, "-", "")))
	if len(c) != 8 {
		return false
	}
	for _, r := range c {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// ---- 状态与开启 / 关闭 ----

// TwofaStatus GET /api/me/2fa —— 本人两步验证状态（设置页展示）。
func (h *AuthHandler) TwofaStatus(c *gin.Context) {
	me := middleware.UserFrom(c)
	c.JSON(http.StatusOK, gin.H{
		"enabled": me.TwofaEnabled,
		"method":  me.TwofaMethod,
		"email":   me.Email != nil && *me.Email != "",
	})
}

// TwofaTotpSetup POST /api/me/2fa/totp/setup —— 生成 TOTP 密钥，返回 otpauth URL
// 供验证器 App 扫码（前端本地渲染二维码）。此时尚未启用，enable 校验通过才落开关。
func (h *AuthHandler) TwofaTotpSetup(c *gin.Context) {
	me := middleware.UserFrom(c)
	if me.TwofaEnabled {
		fail(c, http.StatusConflict, "两步验证已开启，请先关闭再重新配置")
		return
	}
	secret, err := service.TwofaGenerateSecret()
	if err != nil {
		internal(c, err)
		return
	}
	me.TwofaSecret = secret
	if err := h.users.Save(me); err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"secret":     secret,
		"otpauthUrl": service.TwofaOtpauthURL(secret, me.Username),
	})
}

// TwofaTotpEnable POST /api/me/2fa/totp/enable —— 校验 6 位码后开启验证器应用方式，
// 同时生成 8 个一次性恢复码（明文仅此一次下发）。
func (h *AuthHandler) TwofaTotpEnable(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	if me.TwofaSecret == "" {
		fail(c, http.StatusBadRequest, "请先生成密钥并扫码")
		return
	}
	if !service.TwofaVerify(me.TwofaSecret, req.Code, time.Now()) {
		fail(c, http.StatusBadRequest, "验证码不正确")
		return
	}
	codes, err := h.twofaCodes.GenerateForUser(me.ID, store.TwofaRecoveryCount)
	if err != nil {
		internal(c, err)
		return
	}
	me.TwofaEnabled = true
	me.TwofaMethod = "app"
	if err := h.users.Save(me); err != nil {
		internal(c, err)
		return
	}
	slog.Info("2fa enabled (totp)", "uid", me.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "recoveryCodes": codes})
}

// TwofaEmailCode POST /api/me/2fa/email/code —— 向本人邮箱发送两步验证码（开启与登录共用）。
func (h *AuthHandler) TwofaEmailCode(c *gin.Context) {
	me := middleware.UserFrom(c)
	if me.Email == nil || *me.Email == "" {
		fail(c, http.StatusBadRequest, "账号未绑定邮箱，无法使用邮箱验证码")
		return
	}
	if !h.mailerReady() {
		fail(c, http.StatusServiceUnavailable, "站点尚未配置邮件服务，暂时无法发送验证码")
		return
	}
	email := strings.ToLower(strings.TrimSpace(*me.Email))
	code, retryAfter, err := h.emailCodes.Issue(email, twofaEmailPurpose)
	if err != nil {
		internal(c, err)
		return
	}
	if code == "" {
		// 冷却期内：仍 200 并返回剩余秒数（与注册发码口径一致）
		c.JSON(http.StatusOK, gin.H{"ok": true, "cooldownSeconds": int(retryAfter.Seconds()) + 1})
		return
	}
	if err := h.mailer.SendVerifyCode(email, code, store.EmailCodeTTL); err != nil {
		fail(c, http.StatusServiceUnavailable, "验证邮件发送失败，请联系管理员检查邮件服务")
		return
	}
	slog.Info("2fa email code sent", "uid", me.ID, "ip", c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"ok": true, "cooldownSeconds": int(store.EmailCodeCooldown.Seconds())})
}

// TwofaEmailEnable POST /api/me/2fa/email/enable —— 校验邮箱验证码后开启邮箱方式。
func (h *AuthHandler) TwofaEmailEnable(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	if me.Email == nil || *me.Email == "" {
		fail(c, http.StatusBadRequest, "账号未绑定邮箱，无法使用邮箱验证码")
		return
	}
	okCode, err := h.emailCodes.Verify(strings.ToLower(strings.TrimSpace(*me.Email)), twofaEmailPurpose, req.Code)
	if err != nil {
		internal(c, err)
		return
	}
	if !okCode {
		fail(c, http.StatusBadRequest, "验证码不正确或已过期")
		return
	}
	codes, err := h.twofaCodes.GenerateForUser(me.ID, store.TwofaRecoveryCount)
	if err != nil {
		internal(c, err)
		return
	}
	me.TwofaEnabled = true
	me.TwofaMethod = "email"
	if err := h.users.Save(me); err != nil {
		internal(c, err)
		return
	}
	slog.Info("2fa enabled (email)", "uid", me.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "recoveryCodes": codes})
}

// TwofaDisable POST /api/me/2fa/disable —— 关闭两步验证。需要密码再认证
// （第二因子丢失时用户仍可凭密码关闭，不会永久锁死）。
func (h *AuthHandler) TwofaDisable(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	if !me.TwofaEnabled {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	if req.Password == "" || bcrypt.CompareHashAndPassword([]byte(me.PasswordHash), []byte(req.Password)) != nil {
		fail(c, http.StatusBadRequest, "密码不正确")
		return
	}
	me.TwofaEnabled = false
	me.TwofaMethod = ""
	me.TwofaSecret = ""
	if err := h.users.Save(me); err != nil {
		internal(c, err)
		return
	}
	if err := h.twofaCodes.DeleteForUser(me.ID); err != nil {
		internal(c, err)
		return
	}
	slog.Info("2fa disabled", "uid", me.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- 登录期校验（免鉴权，靠 2FA 票据） ----

// TwofaEmailCodeForLogin POST /api/auth/2fa/email-code —— 登录两步验证阶段发邮箱验证码。
func (h *AuthHandler) TwofaEmailCodeForLogin(c *gin.Context) {
	var req struct {
		Ticket string `json:"ticket"`
	}
	if !bind(c, &req) {
		return
	}
	uid, alive := twofaTakeTicket(strings.TrimSpace(req.Ticket))
	if !alive {
		fail(c, http.StatusBadRequest, "登录会话已失效，请重新登录")
		return
	}
	u, err := h.users.ByID(uid)
	if err != nil {
		internal(c, err)
		return
	}
	if u == nil || u.Email == nil || *u.Email == "" {
		fail(c, http.StatusBadRequest, "账号未绑定邮箱，请使用验证器应用或恢复码")
		return
	}
	if !h.mailerReady() {
		fail(c, http.StatusServiceUnavailable, "站点尚未配置邮件服务，暂时无法发送验证码")
		return
	}
	email := strings.ToLower(strings.TrimSpace(*u.Email))
	code, retryAfter, err := h.emailCodes.Issue(email, twofaEmailPurpose)
	if err != nil {
		internal(c, err)
		return
	}
	if code == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true, "cooldownSeconds": int(retryAfter.Seconds()) + 1})
		return
	}
	if err := h.mailer.SendVerifyCode(email, code, store.EmailCodeTTL); err != nil {
		fail(c, http.StatusServiceUnavailable, "验证邮件发送失败，请联系管理员检查邮件服务")
		return
	}
	slog.Info("2fa login email code sent", "uid", uid, "ip", c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"ok": true, "cooldownSeconds": int(store.EmailCodeCooldown.Seconds())})
}

// TwofaVerify POST /api/auth/2fa/verify —— 校验第二因子并签发 JWT（免鉴权，靠票据）。
// code 的形态决定走哪条路：6 位 = TOTP / 邮箱验证码（按用户开启的方式）；
// 8 位十六进制（可带连字符）= 恢复码（两种方式都可用）。
func (h *AuthHandler) TwofaVerify(c *gin.Context) {
	var req struct {
		Ticket string `json:"ticket"`
		Code   string `json:"code"`
	}
	if !bind(c, &req) {
		return
	}
	ticket := strings.TrimSpace(req.Ticket)
	uid, alive := twofaTakeTicket(ticket)
	if !alive {
		fail(c, http.StatusBadRequest, "登录会话已失效，请重新登录")
		return
	}
	u, err := h.users.ByID(uid)
	if err != nil {
		internal(c, err)
		return
	}
	if u == nil || u.IsBanned() {
		twofaDropTicket(ticket)
		fail(c, http.StatusForbidden, "账号已被封禁或不存在")
		return
	}

	code := strings.TrimSpace(req.Code)
	verified := false
	switch {
	case twofaRecoveryFormat(code):
		// 恢复码：单次有效，两种方式都可用；命中即核销
		okCode, rErr := h.twofaCodes.Consume(u.ID, code)
		if rErr != nil {
			internal(c, rErr)
			return
		}
		verified = okCode
		if verified {
			slog.Info("2fa login via recovery code", "uid", u.ID)
		}
	case len(code) == service.TwofaDigits && u.TwofaMethod == "app":
		if u.TwofaSecret == "" {
			fail(c, http.StatusBadRequest, "两步验证配置异常，请先在设置页关闭后重新开启")
			return
		}
		verified = service.TwofaVerify(u.TwofaSecret, code, time.Now())
	case len(code) == service.TwofaDigits && u.TwofaMethod == "email":
		if u.Email == nil || *u.Email == "" {
			fail(c, http.StatusBadRequest, "账号未绑定邮箱，请使用验证器应用或恢复码")
			return
		}
		verified, err = h.emailCodes.Verify(strings.ToLower(strings.TrimSpace(*u.Email)), twofaEmailPurpose, code)
		if err != nil {
			internal(c, err)
			return
		}
	default:
		fail(c, http.StatusBadRequest, "验证码格式不正确")
		return
	}

	if !verified {
		// 统一文案不区分「码错」与「码过期」，不额外泄露信息
		fail(c, http.StatusBadRequest, "验证码不正确或已过期")
		return
	}

	twofaDropTicket(ticket)
	token, err := middleware.IssueToken(u.ID, u.TokenVersion, h.cfg.JWTSecret)
	if err != nil {
		internal(c, err)
		return
	}
	middleware.SetAuthCookie(c.Writer, c.Request, token)
	slog.Info("2fa login ok", "uid", u.ID)
	c.JSON(http.StatusOK, gin.H{"token": token, "user": u})
}
