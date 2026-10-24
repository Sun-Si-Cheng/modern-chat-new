package config

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// envKeys Load 会读取的全部环境变量。测试期间统一隔离，避免受本机 .env / 环境变量影响。
var envKeys = []string{
	"PORT", "GIN_MODE", "DB_DSN", "JWT_SECRET", "LOG_LEVEL", "MIGRATE",
	"PLUGIN_MAX_SIZE", "FRONTEND_URL", "TRUSTED_PROXIES", "LOG_DIR", "LOG_RETAIN_DAYS",
	"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB",
	"SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASS", "SMTP_FROM",
}

// isolateEnv 清空 Load 相关的环境变量，按 set 设置，测试结束自动还原。
// 注意：本包测试不可并行（环境变量是进程级全局状态）。
func isolateEnv(t *testing.T, set map[string]string) {
	t.Helper()
	saved := make(map[string]*string, len(envKeys))
	for _, k := range envKeys {
		if v, ok := os.LookupEnv(k); ok {
			vv := v
			saved[k] = &vv
		} else {
			saved[k] = nil
		}
		_ = os.Unsetenv(k)
	}
	// 默认用 debug 模式：GIN_MODE 默认是 release，而 release 下未设置 JWT_SECRET 会拒绝启动，
	// 多数用例关注的是别的字段，不应被这条安全限制干扰（需要测它的用例会显式覆盖）。
	if _, ok := set["GIN_MODE"]; !ok {
		_ = os.Setenv("GIN_MODE", "debug")
	}
	for k, v := range set {
		_ = os.Setenv(k, v)
	}
	t.Cleanup(func() {
		for k, v := range saved {
			if v == nil {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, *v)
			}
		}
	})
}

func TestGetv(t *testing.T) {
	isolateEnv(t, nil)

	if got := getv("PORT", "8080"); got != "8080" {
		t.Fatalf("未设置时应取默认值，got %q", got)
	}
	_ = os.Setenv("PORT", "9000")
	if got := getv("PORT", "8080"); got != "9000" {
		t.Fatalf("已设置时应取环境值，got %q", got)
	}
	_ = os.Setenv("PORT", "")
	if got := getv("PORT", "8080"); got != "8080" {
		t.Fatalf("空串应视为未设置并回退默认值，got %q", got)
	}
}

func TestGeti(t *testing.T) {
	isolateEnv(t, nil)

	if got := geti("LOG_RETAIN_DAYS", 15); got != 15 {
		t.Fatalf("未设置时应取默认值，got %d", got)
	}
	_ = os.Setenv("LOG_RETAIN_DAYS", "30")
	if got := geti("LOG_RETAIN_DAYS", 15); got != 30 {
		t.Fatalf("合法数字应被解析，got %d", got)
	}
	_ = os.Setenv("LOG_RETAIN_DAYS", "abc")
	if got := geti("LOG_RETAIN_DAYS", 15); got != 15 {
		t.Fatalf("非法数字应回退默认值，got %d", got)
	}
	_ = os.Setenv("LOG_RETAIN_DAYS", "-1")
	if got := geti("LOG_RETAIN_DAYS", 15); got != -1 {
		t.Fatalf("负数应被解析（由调用方校验），got %d", got)
	}
}

func TestRandomHex(t *testing.T) {
	if got := randomHex(0); got != "" {
		t.Fatalf("randomHex(0) 应为空串，got %q", got)
	}

	s := randomHex(32)
	if len(s) != 64 {
		t.Fatalf("randomHex(32) 应为 64 个十六进制字符，got %d", len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		t.Fatalf("输出应为合法十六进制: %v", err)
	}
	// 随机性：两次结果不应相同
	if randomHex(16) == randomHex(16) {
		t.Fatal("两次随机结果相同，随机源异常")
	}
}

// TestLoadDefaultGinModeIsRelease 安全默认：不设 GIN_MODE 即为生产模式，
// 未配置 JWT_SECRET 时拒绝启动（避免生成随机密钥导致重启即掉线）。
func TestLoadDefaultGinModeIsRelease(t *testing.T) {
	// 显式传空串：既阻止 helper 注入 debug，又让 getv 回退到默认 release
	isolateEnv(t, map[string]string{"GIN_MODE": ""})

	c, err := Load()
	if err == nil {
		t.Fatal("默认 release 模式且未设置 JWT_SECRET 时应拒绝启动")
	}
	if c.GinMode != "release" {
		t.Fatalf("GinMode 默认应为 release，got %q", c.GinMode)
	}
}

func TestLoadDefaults(t *testing.T) {
	isolateEnv(t, map[string]string{"GIN_MODE": "debug"})

	c, err := Load()
	if err != nil {
		t.Fatalf("开发模式未设置 JWT_SECRET 时应自动生成而非报错: %v", err)
	}
	if c.Port != "8080" {
		t.Fatalf("Port 默认应为 8080，got %q", c.Port)
	}
	if c.LogLevel != "info" {
		t.Fatalf("LogLevel 默认应为 info，got %q", c.LogLevel)
	}
	if c.Migrate {
		t.Fatal("MIGRATE 默认应为 false")
	}
	if c.PluginMaxSize != 1<<20 {
		t.Fatalf("PluginMaxSize 默认应为 1MB，got %d", c.PluginMaxSize)
	}
	if c.LogRetainDays != 15 {
		t.Fatalf("LogRetainDays 默认应为 15，got %d", c.LogRetainDays)
	}
	if len(c.FrontendURLs) != 1 || c.FrontendURLs[0] != "http://localhost:5173" {
		t.Fatalf("FrontendURLs 默认应为单机开发地址，got %v", c.FrontendURLs)
	}
	// 未显式设置 TRUSTED_PROXIES 时必须只信任回环：
	// gin 默认信任所有代理，会导致伪造 X-Forwarded-For 绕过按 IP 的限流。
	if len(c.TrustedProxies) != 2 {
		t.Fatalf("未设置 TRUSTED_PROXIES 时应只信任回环，got %v", c.TrustedProxies)
	}
	if c.Redis.Enabled() {
		t.Fatal("未配置 REDIS_ADDR 时 Redis 应禁用")
	}
	if c.SMTP.Enabled() {
		t.Fatal("未配置 SMTP_HOST 时 SMTP 应禁用")
	}
	if len(c.JWTSecret) < 32 {
		t.Fatal("开发模式应自动生成 ≥32 字节的 JWT_SECRET")
	}
}

func TestLoadFrontendURLs(t *testing.T) {
	isolateEnv(t, map[string]string{"FRONTEND_URL": "https://a.example.com/, https://b.example.com/ ,,"})

	c, err := Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if len(c.FrontendURLs) != 2 {
		t.Fatalf("应解析出 2 个来源（空项被跳过），got %v", c.FrontendURLs)
	}
	if c.FrontendURLs[0] != "https://a.example.com" || c.FrontendURLs[1] != "https://b.example.com" {
		t.Fatalf("应去掉尾部斜杠与两侧空格，got %v", c.FrontendURLs)
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	// 显式设为空串 = 不信任任何代理（不使用 XFF 判定 ClientIP）
	isolateEnv(t, map[string]string{"TRUSTED_PROXIES": ""})
	c, err := Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if len(c.TrustedProxies) != 0 {
		t.Fatalf("显式设为空应得到空列表，got %v", c.TrustedProxies)
	}

	// 显式网段
	isolateEnv(t, map[string]string{"TRUSTED_PROXIES": "10.0.0.0/8, 172.16.0.1"})
	c, err = Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[0] != "10.0.0.0/8" || c.TrustedProxies[1] != "172.16.0.1" {
		t.Fatalf("应按逗号解析并去空格，got %v", c.TrustedProxies)
	}
}

func TestLoadJWTSecretRules(t *testing.T) {
	// 生产模式未设置 → 拒绝启动
	isolateEnv(t, map[string]string{"GIN_MODE": "release"})
	if _, err := Load(); err == nil {
		t.Fatal("生产模式未设置 JWT_SECRET 应报错")
	}

	// 长度不足 → 报错（与模式无关）
	isolateEnv(t, map[string]string{"GIN_MODE": "debug", "JWT_SECRET": "too-short"})
	if _, err := Load(); err == nil {
		t.Fatal("JWT_SECRET 短于 32 字节应报错")
	}

	// 合法值 → 原样保留
	secret := strings.Repeat("a", 32)
	isolateEnv(t, map[string]string{"GIN_MODE": "release", "JWT_SECRET": secret})
	c, err := Load()
	if err != nil {
		t.Fatalf("合法 JWT_SECRET 不应报错: %v", err)
	}
	if c.JWTSecret != secret {
		t.Fatalf("JWT_SECRET 应原样保留，got %q", c.JWTSecret)
	}
}

func TestEnabledFlags(t *testing.T) {
	if (RedisConfig{}).Enabled() {
		t.Fatal("Addr 为空时 Redis 应禁用")
	}
	if !(RedisConfig{Addr: "127.0.0.1:6379"}).Enabled() {
		t.Fatal("有 Addr 时 Redis 应启用")
	}
	if (SMTPConfig{}).Enabled() {
		t.Fatal("Host 为空时 SMTP 应禁用")
	}
	if !(SMTPConfig{Host: "smtp.qq.com"}).Enabled() {
		t.Fatal("有 Host 时 SMTP 应启用")
	}
}
