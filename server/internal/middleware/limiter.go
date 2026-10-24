package middleware

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// LimiterStore 内存令牌桶集合（按 key 分桶），周期性整体清理。
type LimiterStore struct {
	mu     sync.Mutex
	items  map[string]*rate.Limiter
	perMin rate.Limit
	burst  int
}

// NewLimiterStore 每分钟 perMin 次、突发 burst；后台每 10 分钟清理一次。
func NewLimiterStore(perMin, burst int) *LimiterStore {
	l := &LimiterStore{
		items:  make(map[string]*rate.Limiter),
		perMin: rate.Limit(float64(perMin) / 60.0),
		burst:  burst,
	}
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			l.mu.Lock()
			l.items = make(map[string]*rate.Limiter)
			l.mu.Unlock()
			slog.Debug("limiter store cleared")
		}
	}()
	return l
}

// Allow 是否放行。
func (l *LimiterStore) Allow(key string) bool {
	l.mu.Lock()
	lim, ok := l.items[key]
	if !ok {
		lim = rate.NewLimiter(l.perMin, l.burst)
		l.items[key] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}

// RateLimit REST 限流中间件；keyFn 决定按 IP 还是按用户。
func RateLimit(l *LimiterStore, keyFn func(c *gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.Allow(keyFn(c)) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "操作过于频繁，请稍后再试"})
			return
		}
		c.Next()
	}
}

// KeyByIP 按 IP 限流（登录/注册/找回）。
func KeyByIP(c *gin.Context) string { return "ip:" + c.ClientIP() }
