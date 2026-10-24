package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
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
}

func NewAuthHandler(cfg config.Config, users *store.UserStore, resets *store.PasswordResetStore, mailer *service.Mailer) *AuthHandler {
	return &AuthHandler{cfg: cfg, users: users, resets: resets, mailer: mailer}
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Register 注册。首个注册者 role=admin；avatar_color 按 id 取色板。
func (h *AuthHandler) Register(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
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
	// 插入与站长选举在同一事务内完成：并发注册不会产生多个站长，
	// 头像取色也在同一事务内按自增 id 回填。
	if err := h.users.CreateWithAdminElection(u); err != nil {
		// 并发同名注册落到唯一索引
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			fail(c, http.StatusConflict, "用户名已被占用")
			return
		}
		internal(c, err)
		return
	}

	token, err := middleware.IssueToken(u.ID, u.TokenVersion, h.cfg.JWTSecret)
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("user registered", "uid", u.ID, "role", u.Role)
	c.JSON(http.StatusOK, gin.H{"token": token, "user": u})
}

// Login 登录；封禁用户直接拒绝。
func (h *AuthHandler) Login(c *gin.Context) {
	var req credentials
	if !bind(c, &req) {
		return
	}
	u, err := h.users.ByUsername(strings.TrimSpace(req.Username))
	if err != nil {
		internal(c, err)
		return
	}
	if u == nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		// 登录失败是安全审计要点（撞库/暴力破解的早期信号），只记用户名与来源，不记密码
		slog.Warn("login failed", "username", req.Username, "ip", c.ClientIP())
		fail(c, http.StatusBadRequest, "用户名或密码错误")
		return
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
		// FRONTEND_URL 可能被配置成空串（切分后 FrontendURLs 为空），
		// 原实现直接取 [0] 会索引越界 panic；此处判空后跳过发信并告警。
		if len(h.cfg.FrontendURLs) == 0 {
			slog.Warn("password reset link skipped: FRONTEND_URL not configured", "uid", u.ID)
		} else {
			link := h.cfg.FrontendURLs[0] + "/forgot?token=" + token
			// 异步发送：SMTP 建连最长可达 10s，同步发送会让「邮箱存在」的请求显著变慢，
			// 从而通过耗时差异枚举出已注册邮箱；异步后响应耗时恒定。
			go h.mailer.SendResetLink(req.Email, link)
		}
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
	// 昵称/头像属于公开资料：广播给好友与同会话成员，前端就地更新各处头像
	if req.Nickname != nil || req.Avatar != nil {
		notifyFor(c).NotifyUserChange(u.ID, u.Nickname, u.Avatar, u.AvatarColor)
	}
	c.JSON(http.StatusOK, u)
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
