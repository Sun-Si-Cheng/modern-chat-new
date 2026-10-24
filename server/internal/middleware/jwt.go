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
		c.Set("user", u)
		c.Set("uid", u.ID)
		c.Next()
	}
}

func abort401(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已失效，请重新登录"})
}
