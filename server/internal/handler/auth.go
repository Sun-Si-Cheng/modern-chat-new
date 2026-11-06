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

	// 只需要布尔语义（是否首个账号）：用 EXISTS 而不是 COUNT(*)，
	// 注册路径同样不该为一次判空扫全表
	hasUsers, err := h.users.HasUsers()
	if err != nil {
		internal(c, err)
		return
	}

	// 邮箱：开关开启时**必填并落库**——首个账号也要存，否则该账号永远无法走
	// 「忘记密码」（只能改库或让站长重置）。验证码只在「非首账号 && SMTP 可用」
	// 时才要求：首账号豁免，避免把刚部署完的部署者锁在注册页外。
	// 放在邀请码之后：验证失败时上面的 defer 会把已核销的邀请码退还。
	needCode := ops.RequireEmailVerify && h.mailerReady() && hasUsers
	var emailPtr *string
	emailVerified := false
	if ops.RequireEmailVerify {
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
		if needCode {
			if h.emailCodes == nil {
				internal(c, fmt.Errorf("email code store not initialized"))
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
			emailVerified = true
		}
		emailPtr = &email
	}
	if ops.RequireEmailVerify && !hasUsers {
		slog.Info("first account skips email verification (bootstrap)")
	}
	if ops.RequireEmailVerify && !h.mailerReady() {
		slog.Warn("email verification is on but SMTP is not configured; registration falls back to no verification")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		internal(c, err)
		return
	}

	u := &model.User{
		Username:     req.Username,
		PasswordHash: string(hash),
		Nickname:     nickname,
	}
	if emailPtr != nil {
		u.Email = emailPtr
		u.EmailVerified = emailVerified
	}
	// 建号与「首任站长选举」在同一临界区/事务里完成：并发首注册不会各插一个 admin
	if err := h.users.CreateWithAdminElection(u); err != nil {
		// 并发同名注册落到唯一索引
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			fail(c, http.StatusConflict, "用户名已被占用")
			return
		}
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
	// 与 ConversationHandler.initInboxes 保持一致：新注册用户进公共群时同步
	// 当前最新消息作为预览，否则会话列表显示「暂无消息」而聊天里却有历史。
	row := model.UserConversation{UserID: userID, ConversationID: conv.ID}
	if latestID, latestAt, err := h.convs.LatestVisibleMessage(conv.ID); err == nil && latestID > 0 {
		row.LastMessageID = &latestID
		at := latestAt
		row.LastMessageAt = &at
		row.LastReadMessageID = latestID
	}
	if err := h.msgs.UpsertInbox([]model.UserConversation{row}); err != nil {
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
		h.mailer.SendResetLink(req.Email, link, token)
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

// UpdateMe 改昵称 / 头像 / 邮箱。
//
// 换绑邮箱必须带新邮箱收到的验证码并核销通过：只校验唯一性就落库的话，
// 任何人都能把资料邮箱改成别人的地址（抢占唯一索引 + 让重置链接寄给别人）。
func (h *AuthHandler) UpdateMe(c *gin.Context) {
	var req struct {
		Nickname *string `json:"nickname"`
		Email    *string `json:"email"`
		Avatar   *string `json:"avatar"`
		// EmailCode 换绑邮箱时必填：发到新邮箱的 6 位验证码
		EmailCode string `json:"emailCode"`
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
		if e == "" {
			// 解绑：不需要验证（清掉验证状态）
			u.Email = nil
			u.EmailVerified = false
		} else if u.Email == nil || *u.Email != e {
			// 换绑：必须验证新邮箱归属
			if !h.mailerReady() {
				fail(c, http.StatusBadRequest, "站点未配置邮件服务，暂不支持修改邮箱")
				return
			}
			existing, err := h.users.ByEmail(e)
			if err != nil {
				internal(c, err)
				return
			}
			if existing != nil && existing.ID != u.ID {
				fail(c, http.StatusConflict, "邮箱已被绑定")
				return
			}
			code := strings.TrimSpace(req.EmailCode)
			if code == "" {
				fail(c, http.StatusBadRequest, "请先获取新邮箱的验证码")
				return
			}
			if h.emailCodes == nil {
				internal(c, fmt.Errorf("email code store not initialized"))
				return
			}
			ok, err := h.emailCodes.Verify(e, store.EmailVerifyPurposeChange, code)
			if err != nil {
				internal(c, err)
				return
			}
			if !ok {
				fail(c, http.StatusUnprocessableEntity, "验证码错误或已过期，请重新获取")
				return
			}
			u.Email = &e
			u.EmailVerified = true
		}
		// 邮箱没变化：什么都不做（不改验证状态，避免「只改昵称」把它清掉）
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

// EmailCodeChange 发送换绑邮箱验证码（登录后；验证码发往**新邮箱**）。
//
// 与注册发码分开：注册要检查「站点是否开放注册」，换绑只需要登录态，
// 且必须排除「新邮箱与当前邮箱相同」「新邮箱已被别人占用」。
func (h *AuthHandler) EmailCodeChange(c *gin.Context) {
	if !h.mailerReady() {
		fail(c, http.StatusServiceUnavailable, "站点未配置邮件服务，暂不支持修改邮箱")
		return
	}
	u := middleware.UserFrom(c)
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
	if u.Email != nil && *u.Email == email {
		fail(c, http.StatusBadRequest, "新邮箱与当前邮箱相同")
		return
	}
	if existing, err := h.users.ByEmail(email); err != nil {
		internal(c, err)
		return
	} else if existing != nil && existing.ID != u.ID {
		fail(c, http.StatusConflict, "邮箱已被绑定")
		return
	}
	if h.emailCodes == nil {
		internal(c, fmt.Errorf("email code store not initialized"))
		return
	}

	code, retryAfter, err := h.emailCodes.Issue(email, store.EmailVerifyPurposeChange)
	if err != nil {
		internal(c, err)
		return
	}
	if code == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true, "cooldownSeconds": int(retryAfter.Seconds()) + 1})
		return
	}
	if err := h.mailer.SendVerifyCode(email, code, store.EmailCodeTTL); err != nil {
		fail(c, http.StatusServiceUnavailable, "验证邮件发送失败，请稍后重试")
		return
	}
	slog.Info("email change code sent", "uid", u.ID, "email", email)
	c.JSON(http.StatusOK, gin.H{"ok": true, "cooldownSeconds": int(store.EmailCodeCooldown.Seconds())})
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
