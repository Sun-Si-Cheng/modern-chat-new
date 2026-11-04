// Package config 负责环境变量解析与默认值（见 .env.example）。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config 全部运行配置，来源仅环境变量。
type Config struct {
	Port           string
	GinMode        string
	DBDSN          string
	JWTSecret      string
	LogLevel       string
	Migrate        bool
	PluginMaxSize  int64
	FrontendURLs   []string // 允许的前端来源（WS Origin 校验 + 找回密码链接）
	TrustedProxies []string // 可信反向代理网段；空表示不信任任何代理（不使用 XFF 判定 ClientIP）
	LogDir         string   // 日志目录；留空则只输出 stdout
	DataDir        string   // 数据目录（图片/语音等上传文件），默认 ./data
	LogRetainDays  int      // 日志保留天数（默认 15）
	Redis          RedisConfig
	SMTP           SMTPConfig
}

// RedisConfig 可选：配置后限流使用 Redis 共享配额，连接失败自动降级为内存。
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// Enabled 是否配置了 Redis（Addr 为空即禁用）。
func (r RedisConfig) Enabled() bool { return r.Addr != "" }

// SMTPConfig 找回密码邮件发送；为空时打日志（开发期）。
type SMTPConfig struct {
	Host, Port, User, Pass, From string
	// LogResetLink 未配置 SMTP 时，是否把完整的重置链接写进日志。
	// 默认仅在非 release 模式开启：日志里出现可用令牌等于把账户接管入口
	// 暴露给所有能看日志的人（含日志采集平台）。
	LogResetLink bool
}

func (c SMTPConfig) Enabled() bool { return c.Host != "" }

// Load 读取环境变量。JWT_SECRET 规则：
//   - 生产（GIN_MODE=release）必须显式设置且 ≥32 字节，否则拒绝启动；
//   - 开发模式缺省时生成随机值并告警（重启即全端失效）。
func Load() (Config, error) {
	cfg := Config{
		Port:          getv("PORT", "8080"),
		GinMode:       getv("GIN_MODE", "release"),
		DBDSN:         getv("DB_DSN", "root:root@tcp(127.0.0.1:3306)/modernchat?charset=utf8mb4&parseTime=True&loc=Local"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		LogLevel:      getv("LOG_LEVEL", "info"),
		Migrate:       getv("MIGRATE", "false") == "true",
		PluginMaxSize: geti("PLUGIN_MAX_SIZE", 1<<20),
		LogDir:        getv("LOG_DIR", "logs"),
		DataDir:       getv("DATA_DIR", "data"),
		LogRetainDays: int(geti("LOG_RETAIN_DAYS", 15)),
		Redis: RedisConfig{
			Addr:     os.Getenv("REDIS_ADDR"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       int(geti("REDIS_DB", 0)),
		},
		SMTP: SMTPConfig{
			Host: os.Getenv("SMTP_HOST"),
			Port: getv("SMTP_PORT", "587"),
			User: os.Getenv("SMTP_USER"),
			Pass: os.Getenv("SMTP_PASS"),
			From: getv("SMTP_FROM", "modernchat@localhost"),
			// 生产默认不把可用令牌写进日志；本地开发保留便利
			LogResetLink: getBool("LOG_RESET_LINK", getv("GIN_MODE", "release") != "release"),
		},
	}
	// 前端来源：逗号分隔，用于 WS Origin 白名单与找回密码链接。
	// 必须先 TrimSpace 再 TrimSuffix：配置项写成 "https://a.com/, https://b.com/ " 时，
	// 第二个值的尾部是空格，若先 TrimSuffix 会漏掉斜杠 —— 浏览器 Origin 不带尾斜杠，
	// 残留的斜杠会让 WS 握手被 Origin 白名单拒绝。
	for _, u := range strings.Split(getv("FRONTEND_URL", "http://localhost:5173"), ",") {
		if u = strings.TrimSuffix(strings.TrimSpace(u), "/"); u != "" {
			cfg.FrontendURLs = append(cfg.FrontendURLs, u)
		}
	}
	// 可信反向代理：默认只信任本机回环（nginx 同机部署）。
	// 修复缺陷：gin 默认信任所有代理，攻击者伪造 X-Forwarded-For 即可换 IP，
	// 绕过按 IP 的登录/注册限流。显式设为空串（TRUSTED_PROXIES=）表示不信任任何代理。
	if v, ok := os.LookupEnv("TRUSTED_PROXIES"); ok {
		if v = strings.TrimSpace(v); v == "" {
			cfg.TrustedProxies = []string{}
		} else {
			for _, p := range strings.Split(v, ",") {
				if p = strings.TrimSpace(p); p != "" {
					cfg.TrustedProxies = append(cfg.TrustedProxies, p)
				}
			}
		}
	} else {
		cfg.TrustedProxies = []string{"127.0.0.1", "::1"}
	}
	if len(cfg.JWTSecret) < 32 {
		if cfg.JWTSecret != "" {
			return cfg, fmt.Errorf("JWT_SECRET 长度不足 32 字节")
		}
		if cfg.GinMode == "release" {
			return cfg, fmt.Errorf("生产模式（GIN_MODE=release）必须显式设置 JWT_SECRET（≥32 字节，如 openssl rand -hex 32）")
		}
		cfg.JWTSecret = randomHex(32)
		fmt.Fprintln(os.Stderr, "WARN: JWT_SECRET 未设置，已生成随机值（重启后所有登录态失效；仅限本地开发）")
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

// getBool 解析布尔环境变量；空值或无法识别时回退 def。
func getBool(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}
