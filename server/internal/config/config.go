// Package config 负责环境变量解析与默认值（见 .env.example）。
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config 全部运行配置，来源仅环境变量。
type Config struct {
	Port          string
	GinMode       string
	DBDSN         string
	JWTSecret     string
	LogLevel      string
	Migrate       bool
	PluginMaxSize int64
	SMTP          SMTPConfig
}

// SMTPConfig 找回密码邮件发送；为空时打日志（开发期）。
type SMTPConfig struct {
	Host, Port, User, Pass, From string
}

func (c SMTPConfig) Enabled() bool { return c.Host != "" }

// Load 读取环境变量；JWT_SECRET 缺省时生成随机值并告警（仅限开发，重启即全端失效）。
func Load() (Config, error) {
	cfg := Config{
		Port:          getv("PORT", "8080"),
		GinMode:       getv("GIN_MODE", "release"),
		DBDSN:         getv("DB_DSN", "root:root@tcp(127.0.0.1:3306)/modernchat?charset=utf8mb4&parseTime=True&loc=Local"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		LogLevel:      getv("LOG_LEVEL", "info"),
		Migrate:       getv("MIGRATE", "false") == "true",
		PluginMaxSize: geti("PLUGIN_MAX_SIZE", 1<<20),
		SMTP: SMTPConfig{
			Host: os.Getenv("SMTP_HOST"),
			Port: getv("SMTP_PORT", "587"),
			User: os.Getenv("SMTP_USER"),
			Pass: os.Getenv("SMTP_PASS"),
			From: getv("SMTP_FROM", "modernchat@localhost"),
		},
	}
	if len(cfg.JWTSecret) < 32 {
		if cfg.JWTSecret != "" {
			return cfg, fmt.Errorf("JWT_SECRET 长度不足 32 字节")
		}
		cfg.JWTSecret = randomHex(32)
		fmt.Fprintln(os.Stderr, "WARN: JWT_SECRET 未设置，已生成随机值（重启后所有登录态失效；生产环境必须显式设置）")
	}
	return cfg, nil
}

func getv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func geti(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
