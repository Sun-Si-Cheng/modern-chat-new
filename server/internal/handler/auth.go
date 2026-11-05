package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// AuthHandler 认证相关。
type AuthHandler struct {
	cfg    config.Config
	users  *store.UserStore
	resets *store.PasswordResetStore
	mailer *service.Mailer
	convs  *store.ConversationStore
	msgs   *store.MessageStore
	// settings 用于读取「是否开放注册」这类可运营开关（可为 nil）
	settings *store.SettingStore
	// invites 邀请码核销（注册需要邀请码时使用）
	invites *store.InviteStore
	// loginAttempts 账号级登录失败锁定
	loginAttempts *store.LoginAttemptStore
	// emailCodes 注册邮箱验证码
	emailCodes *store.EmailCodeStore
}

func NewAuthHandler(
	cfg config.Config,
	users *store.UserStore,
	resets *store.PasswordResetStore,
	mailer *service.Mailer,
	convs *store.ConversationStore,
	msgs *store.MessageStore,
	settings *store.SettingStore,
	invites *store.InviteStore,
	loginAttempts *store.LoginAttemptStore,
	emailCodes *store.EmailCodeStore,
) *AuthHandler {
	return &AuthHandler{
		cfg: cfg, users: users, resets: resets, mailer: mailer,
		convs: convs, msgs: msgs, settings: settings,
		invites: invites, loginAttempts: loginAttempts,
		emailCodes: emailCodes,
	}
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Register 注册。首个注册者 role=admin；avatar_color 按 id 取色板。
//
// 开启「注册需邮箱验证」时，必须带 email + 邮箱收到的 6 位验证码；
// 验证码在创建用户前核销（邀请码先核销、失败自动退还，见下方 defer）。
func (h *AuthHandler) Register(c *gin.Context) {
	// 站点开关：关闭后仍保留已有账号登录，只是不再接受新注册
	ops := store.DefaultOps()
	if h.settings != nil {
		ops = h.settings.Ops()
	}
	if !ops.AllowRegister {
		fail(c, http.StatusForbidden, "站点已关闭注册，请联系管理员")
		return
	}

	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		Nickname   string `json:"nickname"`
		InviteCode string `json:"inviteCode"`
		Email      string `json:"email"`
		EmailCode  string `json:"emailCode"`
	}
	if !bind(c, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if msg := validateUsername(req.Username); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	if msg := validatePassword(req.Password); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	nickname := strings.TrimSpace(req.Nickname)
	if msg := validateNickname(nickname); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}

	if existing, err := h.users.ByUsername(req.Username); err != nil {
		internal(c, err)
		return
	} else if existing != nil {
		fail(c, http.StatusConflict, "用户名已被占用")
		return
	}

	// 邀请码：先条件 UPDATE 原子核销，再创建用户；注册中途失败则退还名额。
	// 不能「先查再用」——并发下最后一个名额会被多人同时用掉。
	inviteUsed := ""
	registrationDone := false
	defer func() {
		if inviteUsed != "" && !registrationDone && h.invites != nil {
			if err := h.invites.Refund(inviteUsed); err != nil {
				slog.Error("refund invite failed", "err", err.Error())
			}
		}
	}()
	if ops.RequireInviteCode {
		code := strings.TrimSpace(req.InviteCode)
		if code == "" {
			fail(c, http.StatusBadRequest, "请填写邀请码")
			return
		}
		if h.invites == nil {
			internal(c, fmt.Errorf("invite store not initialized"))
			return
		}
		ok, err := h.invites.Consume(code)
		if err != nil {
			internal(c, err)
			return
		}
		if !ok {
			fail(c, http.StatusBadRequest, "邀请码无效、已用完或已过期")
			return
		}
		inviteUsed = code
	}

	count, err := h.users.Count()
	if err != nil {
		internal(c, err)
		return
	}

	// 邮箱验证：必须带邮箱与邮件里的 6 位验证码，核销通过才继续。
	// 放在邀请码之后：验证失败时上面的 defer 会把已核销的邀请码退还。
	//
	// 生效条件 = 后台开关 && SMTP 可用 && 不是首个账号：
	//  - 没配 SMTP 却强制验证，等于把所有人都挡在注册页外面（含刚部署完的站长）
	//  - 首个账号豁免，保证部署者永远能把站点引导起来
	var emailVerified *string
	requireEmail := ops.RequireEmailVerify && h.mailerReady() && count > 0
	if ops.RequireEmailVerify && count == 0 {
		slog.Info("first account skips email verification (bootstrap)")
	}
	if ops.RequireEmailVerify && !h.mailerReady() {
		slog.Warn("email verification is on but SMTP is not configured; registration falls back to no verification")
	}
	if requireEmail {
		if h.emailCodes == nil {
			internal(c, fmt.Errorf("email code store not initialized"))
			return
		}
		email := service.NormalizeEmail(req.Email)
		if email == "" {
			fail(c, http.StatusBadRequest, "请填写邮箱")
			return
		}
		if msg := validateEmail(email); msg != "" {
			fail(c, http.StatusBadRequest, msg)
			return
		}
		if existing, err := h.users.ByEmail(email); err != nil {
			internal(c, err)
			return
		} else if existing != nil {
			fail(c, http.StatusConflict, "邮箱已被绑定")
			return
		}
		code := strings.TrimSpace(req.EmailCode)
		if code == "" {
			fail(c, http.StatusBadRequest, "请填写邮箱验证码")
			return
		}
		ok, err := h.emailCodes.Verify(email, store.EmailVerifyPurposeRegister, code)
		if err != nil {
			internal(c, err)
			return
		}
		if !ok {
			fail(c, http.StatusUnprocessableEntity, "验证码错误或已过期，请重新获取")
			return
		}
		emailVerified = &email
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		internal(c, err)
		return
	}

	role := "user"
	if count == 0 {
		role = "admin"
	}

	u := &model.User{
		Username:     req.Username,
		PasswordHash: string(hash),
		Nickname:     nickname,
		Role:         role,
	}
	if emailVerified != nil {
		u.Email = emailVerified
		u.EmailVerified = true
	}
	if err := h.users.Create(u); err != nil {
		// 并发同名注册落到唯一索引
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			fail(c, http.StatusConflict, "用户名已被占用")
			return
		}
		internal(c, err)
		return
	}
	// 头像取色依赖自增 id，创建后回填
	u.AvatarColor = model.PickColor(u.ID)
	if err := h.users.Save(u); err != nil {
		internal(c, err)
		return
	}
	// 用户已落库：邀请码不再退还（后续发欢迎消息/签发 token 失败也不该重复占名额）
	registrationDone = true

	// 新用户自动加入初始公开群；失败只记日志，不阻断注册
	h.joinDefaultGroup(u.ID)

	token, err := middleware.IssueToken(u.ID, u.TokenVersion, h.cfg.JWTSecret)
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("user registered", "uid", u.ID, "role", u.Role)
	c.JSON(http.StatusOK, gin.H{"token": token, "user": u})
}

// joinDefaultGroup 新注册用户自动加入初始公开群（「公共聊天室」）。
func (h *AuthHandler) joinDefaultGroup(userID uint) {
	conv, _, err := h.convs.EnsureDefaultGroup()
	if err != nil {
		slog.Warn("default group ensure failed", "uid", userID, "err", err.Error())
		return
	}
	if err := h.convs.JoinGroup(conv.ID, userID, store.MaxGroupMembers); err != nil {
		if !errors.Is(err, store.ErrGroupFull) {
			slog.Warn("join default group failed", "uid", userID, "err", err.Error())
		}
		return
	}
	if err := h.msgs.UpsertInbox([]model.UserConversation{{UserID: userID, ConversationID: conv.ID}}); err != nil {
		slog.Warn("default group inbox failed", "uid", userID, "err", err.Error())
	}
}

// Login 登录；封禁用户直接拒绝。
func (h *AuthHandler) Login(c *gin.Context) {
	var req credentials
	if !bind(c, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)

	// 账号级锁定（阈值配 0 即关闭）；IP 限流在中间件层照常生效，两者互补
	ops := store.DefaultOps()
	if h.settings != nil {
		ops = h.settings.Ops()
	}
	locking := ops.LoginMaxAttempts > 0 && h.loginAttempts != nil
	if locking {
		locked, remaining, lErr := h.loginAttempts.IsLocked(username)
		if lErr != nil {
			internal(c, lErr)
			return
		}
		if locked {
			fail(c, http.StatusTooManyRequests,
				fmt.Sprintf("登录失败次数过多，账号已锁定，请 %d 分钟后重试", int(remaining.Minutes())+1))
			return
		}
	}

	u, err := h.users.ByUsername(username)
	if err != nil {
		internal(c, err)
		return
	}
	if u == nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		if locking {
			if fErr := h.loginAttempts.Fail(username, ops.LoginMaxAttempts, ops.LoginLockMinutes); fErr != nil {
				// 计数失败不该让登录接口变成 500（否则等于给攻击者一个禁用手段）
				slog.Error("record login failure failed", "err", fErr.Error())
			}
		}
		// 登录失败是安全审计要点（撞库/暴力破解的早期信号），只记用户名与来源，不记密码
		slog.Warn("login failed", "username", req.Username, "ip", c.ClientIP())
		fail(c, http.StatusBadRequest, "用户名或密码错误")
		return
	}
	// 凭据正确即清零：封禁与否不影响，避免被封用户解锁后还背着旧计数
	if h.loginAttempts != nil {
		_ = h.loginAttempts.Reset(username)
	}
	if u.IsBanned() {
		slog.Warn("login rejected: banned", "uid", u.ID, "username", u.Username, "ip", c.ClientIP())
		msg := "账号已被封禁"
		if u.BanReason != nil && *u.BanReason != "" {
			msg += "：" + *u.BanReason
		}
		if u.BannedUntil != nil && u.BannedUntil.Year() < 2100 {
			msg += "（至 " + u.BannedUntil.Format("2006-01-02 15:04") + "）"
		}
		fail(c, http.StatusForbidden, msg)
		return
	}
	token, err := middleware.IssueToken(u.ID, u.TokenVersion, h.cfg.JWTSecret)
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("user login", "uid", u.ID)
	c.JSON(http.StatusOK, gin.H{"token": token, "user": u})
}

// dummyHash 恒定时间路径用：用户不存在时也执行一次 bcrypt 比对，抹平主要时序差异。
var dummyHash = func() string {
	h, _ := bcrypt.GenerateFromPassword([]byte("timing-equalizer"), 10)
	return string(h)
}()

// Forgot 提交邮箱：始终返回 ok 且保持近似恒定耗时（不泄露邮箱是否存在）。
func (h *AuthHandler) Forgot(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
	}
	if !bind(c, &req) {
		return
	}
	req.Email = service.NormalizeEmail(req.Email)
	if msg := validateEmail(req.Email); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	u, err := h.users.ByEmail(req.Email)
	if err != nil {
		internal(c, err)
		return
	}
	if u != nil {
		token := randomToken(32)
		pr := &model.PasswordReset{
			UserID:    u.ID,
			Token:     token,
			ExpiresAt: time.Now().Add(15 * time.Minute),
		}
		if err := h.resets.Create(pr); err != nil {
			internal(c, err)
			return
		}
		link := h.cfg.FrontendURLs[0] + "/forgot?token=" + token
		h.mailer.SendResetLink(req.Email, link)
	} else {
		// 均衡耗时：执行一次等价的 bcrypt 比对
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(req.Email))
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Reset 用令牌设新密码；成功后 token_version+1 全端失效。
func (h *AuthHandler) Reset(c *gin.Context) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"newPassword"`
	}
	if !bind(c, &req) {
		return
	}
	if msg := validatePassword(req.NewPassword); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	pr, err := h.resets.Valid(req.Token)
	if err != nil {
		internal(c, err)
		return
	}
	if pr == nil {
		fail(c, http.StatusUnprocessableEntity, "重置令牌无效或已过期")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 10)
	if err != nil {
		internal(c, err)
		return
	}
	u, err := h.users.ByID(pr.UserID)
	if err != nil {
		internal(c, err)
		return
	}
	if u == nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	err = h.resets.Use(pr, func(tx *gorm.DB) error {
		u.PasswordHash = string(hash)
		u.TokenVersion++
		if err := tx.Save(u).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("password reset", "uid", u.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// EmailCode 发送注册邮箱验证码（注册页「发送验证码」）。
//
// 冷却期内不重复发信，但仍回 200 + 剩余秒数：前端据此继续倒计时，
// 也避免该接口被用来探测「这个邮箱是不是刚请求过」。
func (h *AuthHandler) EmailCode(c *gin.Context) {
	ops := store.DefaultOps()
	if h.settings != nil {
		ops = h.settings.Ops()
	}
	if !ops.AllowRegister {
		fail(c, http.StatusForbidden, "站点已关闭注册，请联系管理员")
		return
	}
	if !ops.RequireEmailVerify {
		fail(c, http.StatusBadRequest, "本站未开启邮箱验证")
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if !bind(c, &req) {
		return
	}
	email := service.NormalizeEmail(req.Email)
	if email == "" {
		fail(c, http.StatusBadRequest, "请填写邮箱")
		return
	}
	if msg := validateEmail(email); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	if existing, err := h.users.ByEmail(email); err != nil {
		internal(c, err)
		return
	} else if existing != nil {
		fail(c, http.StatusConflict, "邮箱已被绑定")
		return
	}
	if h.emailCodes == nil {
		internal(c, fmt.Errorf("email code store not initialized"))
		return
	}

	if !h.mailerReady() {
		fail(c, http.StatusServiceUnavailable, "站点尚未配置邮件服务，暂时无法发送验证码")
		return
	}

	code, retryAfter, err := h.emailCodes.Issue(email, store.EmailVerifyPurposeRegister)
	if err != nil {
		internal(c, err)
		return
	}
	if code == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true, "cooldownSeconds": int(retryAfter.Seconds()) + 1})
		return
	}
	if err := h.mailer.SendVerifyCode(email, code, store.EmailCodeTTL); err != nil {
		// 发不出去必须明说：否则用户会一直等一封不会来的邮件
		fail(c, http.StatusServiceUnavailable, "验证邮件发送失败，请联系管理员检查邮件服务")
		return
	}
	slog.Info("email verify code sent", "email", email, "ip", c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"ok": true, "cooldownSeconds": int(store.EmailCodeCooldown.Seconds())})
}

// Me 当前用户（Auth 后）。
func (h *AuthHandler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, middleware.UserFrom(c))
}

// UpdateMe 改昵称 / 邮箱；改邮箱后 email_verified=false。
func (h *AuthHandler) UpdateMe(c *gin.Context) {
	var req struct {
		Nickname *string `json:"nickname"`
		Email    *string `json:"email"`
		Avatar   *string `json:"avatar"`
	}
	if !bind(c, &req) {
		return
	}
	u := middleware.UserFrom(c)

	if req.Avatar != nil {
		a := strings.TrimSpace(*req.Avatar)
		if a != "" && !validAvatarDataURL(a) {
			fail(c, http.StatusBadRequest, "头像格式不支持（仅 png/jpeg/webp，≤256KB）")
			return
		}
		u.Avatar = a
	}

	if req.Nickname != nil {
		n := strings.TrimSpace(*req.Nickname)
		if msg := validateNickname(n); msg != "" {
			fail(c, http.StatusBadRequest, msg)
			return
		}
		u.Nickname = n
	}
	if req.Email != nil {
		e := service.NormalizeEmail(*req.Email)
		if msg := validateEmail(e); msg != "" {
			fail(c, http.StatusBadRequest, msg)
			return
		}
		if e != "" {
			existing, err := h.users.ByEmail(e)
			if err != nil {
				internal(c, err)
				return
			}
			if existing != nil && existing.ID != u.ID {
				fail(c, http.StatusConflict, "邮箱已被绑定")
				return
			}
			u.Email = &e
			u.EmailVerified = false
		} else {
			u.Email = nil
			u.EmailVerified = false
		}
	}
	if err := h.users.Save(u); err != nil {
		internal(c, err)
		return
	}
	// 资料变更实时同步：广播给好友与同会话成员（含本人多端），
	// 否则对方会话列表里的昵称/头像要刷新页面才更新。
	notifyFor(c).NotifyUserChange(u.ID, u.Nickname, u.Avatar, u.AvatarColor)
	c.JSON(http.StatusOK, u)
}

// mailerReady 邮件服务是否可用（未配置 SMTP 时注册不强制验证）。
func (h *AuthHandler) mailerReady() bool {
	return h.mailer != nil && h.mailer.Config().Enabled()
}

func randomToken(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// internal 500 统一响应，细节进日志。
func internal(c *gin.Context, err error) {
	slog.Error("internal error", "path", c.Request.URL.Path, "err", err.Error())
	fail(c, http.StatusInternalServerError, "服务器开小差了，请稍后再试")
}
