package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter 固定窗口限流（按分钟计数），用于多实例共享配额。
//
// 语义：每个 key 每分钟最多 perMin 次；窗口边界可能有最多一个窗口的突发（固定窗口的固有特性）。
// 故障策略：Redis 出错时一律放行（fail-open）——限流是保护手段，不应成为单点故障。
type RedisLimiter struct {
	client *redis.Client
	perMin int
	prefix string
}

func NewRedisLimiter(client *redis.Client, perMin int) *RedisLimiter {
	return &RedisLimiter{client: client, perMin: perMin, prefix: "mc:rl"}
}

func (l *RedisLimiter) Allow(key string) bool {
	bucket := time.Now().Unix() / 60
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	k := fmt.Sprintf("%s:%s:%d", l.prefix, key, bucket)
	n, err := l.client.Incr(ctx, k).Result()
	if err != nil {
		slog.Warn("redis limiter unavailable, allowing request", "err", err.Error())
		return true
	}
	if n == 1 {
		// 首次访问设置过期，避免键无限堆积（略大于窗口）
		_ = l.client.Expire(ctx, k, 70*time.Second).Err()
	}
	return n <= int64(l.perMin)
}
