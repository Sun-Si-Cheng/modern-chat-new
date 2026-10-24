// Package middleware HTTP 中间件：日志、鉴权、封禁、限流。
package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// slowRequest 慢请求阈值：超过则额外告警（排查性能问题的第一手线索）。
const slowRequest = 500 * time.Millisecond

// Logger 结构化请求日志：method path status duration uid ip。
// 级别按结果分级：5xx → error，4xx → warn，慢请求 → warn，其余 info。
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		cost := time.Since(start)
		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", status,
			"duration_ms", cost.Milliseconds(),
			"ip", c.ClientIP(),
		}
		if v, ok := c.Get("uid"); ok {
			attrs = append(attrs, "uid", v)
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "errors", c.Errors.String())
		}

		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		}
		if cost >= slowRequest {
			attrs = append(attrs, "slow", true)
			if level < slog.LevelWarn {
				level = slog.LevelWarn
			}
		}
		slog.Log(c.Request.Context(), level, "http", attrs...)
	}
}
