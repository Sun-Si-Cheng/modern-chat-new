// Package middleware HTTP 中间件：日志、鉴权、封禁、限流。
package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger 结构化请求日志：method path status duration uid ip。
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
		}
		if v, ok := c.Get("uid"); ok {
			attrs = append(attrs, "uid", v)
		}
		level := slog.LevelInfo
		if c.Writer.Status() >= 500 {
			level = slog.LevelError
		}
		slog.Log(c.Request.Context(), level, "http", attrs...)
	}
}
