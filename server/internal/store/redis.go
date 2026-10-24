package store

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// OpenRedis 连接 Redis；addr 为空表示未启用（返回 nil, nil）。
// 连接或 Ping 失败返回 error，调用方应降级为内存实现而不是终止启动。
func OpenRedis(addr, password string, db int) (*redis.Client, error) {
	if addr == "" {
		return nil, nil
	}
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}
