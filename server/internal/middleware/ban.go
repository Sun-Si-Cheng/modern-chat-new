package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Ban 封禁检查（置于 Auth 之后）。
func Ban() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := UserFrom(c)
		if u != nil && u.IsBanned() {
			msg := "账号已被封禁"
			if u.BanReason != nil && *u.BanReason != "" {
				msg += "：" + *u.BanReason
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": msg})
			return
		}
		c.Next()
	}
}

// Admin 站长校验（置于 Ban 之后）。
func Admin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if u := UserFrom(c); u == nil || u.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "无权执行此操作"})
			return
		}
		c.Next()
	}
}
