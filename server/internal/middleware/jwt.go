package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

const TokenTTL = 7 * 24 * time.Hour

// AuthCookieName 与 localStorage 里的 token 是同一个 JWT。
// 浏览器 <img>/<video> 无法附带 Authorization 头，上传文件访问只能靠同源 cookie；
// WebSocket 握手也支持用它替代 query token（避免 token 出现在代理访问日志里）。
const AuthCookieName = "mc_auth"

type userCtxKey struct{}

// UserFrom 从上下文取当前用户（Auth 之后可用）。
func UserFrom(c *gin.Context) *model.User {
	u, _ := c.Get("user")
	if u == nil {
		return nil
	}
	return u.(*model.User)
}

// Claims 解析出的身份。
type Claims struct {
	UID uint
	TV  int64
}

// IssueToken 签发 HS256 JWT（claims: uid, tv）。
func IssueToken(uid uint, tv int64, secret string) (string, error) {
	now := time.Now()
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": uid,
		"tv":  tv,
		"iat": now.Unix(),
		"exp": now.Add(TokenTTL).Unix(),
	})
	return t.SignedString([]byte(secret))
}

// ParseToken 校验并解析 JWT（REST 中间件与 WS 握手共用）。
func ParseToken(tokenStr, secret string) (*Claims, error) {
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil || !tok.Valid {
		return nil, err
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, jwt.ErrTokenInvalidClaims
	}
	uidF, okU := claims["uid"].(float64)
	tvF, okT := claims["tv"].(float64)
	if !okU || !okT {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return &Claims{UID: uint(uidF), TV: int64(tvF)}, nil
}

// Auth 认证中间件：解析 Bearer token、校验 token_version、装载 user。
// 任何失败一律 401（前端统一跳登录）。
func Auth(users *store.UserStore, secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			abort401(c)
			return
		}
		cl, err := ParseToken(strings.TrimPrefix(h, "Bearer "), secret)
		if err != nil {
			abort401(c)
			return
		}
		u, err := users.ByID(cl.UID)
		if err != nil {
			slog.Error("auth load user", "err", err)
			abort401(c)
			return
		}
		if u == nil || u.TokenVersion != cl.TV {
			abort401(c)
			return
		}
		// 顺带写入 HttpOnly cookie，供同源的 <img>/<video> 读取 /uploads 时鉴权。
		// 只用于只读文件访问；REST 接口仍只认 Authorization，避免引入 CSRF 面。
		setAuthCookie(c, strings.TrimPrefix(h, "Bearer "))
		c.Set("user", u)
		c.Set("uid", u.ID)
		c.Next()
	}
}

// UploadAuth 上传文件访问鉴权。
//
// 与 Auth 的区别：额外接受 HttpOnly cookie（浏览器加载 <img>/<video> 时无法带
// Authorization 头）。历史消息里的 /uploads/... 不含签名，登录用户打开旧消息时
// 同源请求会自动带上 cookie，因此不需要迁移数据；未登录访问一律 401。
func UploadAuth(users *store.UserStore, secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ""
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		} else if v, err := c.Cookie(AuthCookieName); err == nil {
			token = v
		}
		if token == "" {
			abort401(c)
			return
		}
		cl, err := ParseToken(token, secret)
		if err != nil {
			abort401(c)
			return
		}
		u, err := users.ByID(cl.UID)
		if err != nil {
			slog.Error("upload auth load user", "err", err)
			abort401(c)
			return
		}
		if u == nil || u.TokenVersion != cl.TV {
			abort401(c)
			return
		}
		if u.IsBanned() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "账号已被封禁"})
			return
		}
		// 用 Authorization 头访问文件的客户端（下载器 / 旧前端）也补发 cookie，
		// 下次 <img>/<video> 直接加载同源地址时即可带上。
		setAuthCookie(c, token)
		c.Set("user", u)
		c.Set("uid", u.ID)
		c.Next()
	}
}

// setAuthCookie 写入与 Bearer token 相同的 HttpOnly cookie。
// Secure 仅在 HTTPS（含反代 X-Forwarded-Proto）时开启，避免 HTTP 部署拿不到 cookie。
func setAuthCookie(c *gin.Context, token string) {
	if v, err := c.Cookie(AuthCookieName); err == nil && v == token {
		return
	}
	SetAuthCookie(c.Writer, c.Request, token)
}

// SetAuthCookie 写入登录 cookie（gin 之外的场景：WS 握手、登录/注册响应）。
// 每次带 Authorization 的已登录请求也会经 Auth 中间件补发一次，
// 因此存量用户（localStorage 有 token、浏览器无 cookie）在升级后
// 第一次调用任意已登录接口就会自动获得 cookie，无需重新登录。
func SetAuthCookie(w http.ResponseWriter, r *http.Request, token string) {
	if token == "" {
		return
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(TokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearAuthCookie 清除登录 cookie（主动退出登录时调用）。
// 只清 localStorage 的话 cookie 仍有效，退出后 7 天内 /uploads 仍能被同浏览器访问。
func ClearAuthCookie(c *gin.Context) {
	secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(AuthCookieName, "", -1, "/", "", secure, true)
}

func abort401(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已失效，请重新登录"})
}
