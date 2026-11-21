package handler

// 旧站迁移接口的纯函数单元测试（不连数据库：Enabled 只看配置，
// guard 只做常量时间比较，excludeIDList 是纯函数）。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/config"
	"github.com/modern-day-team/modern-chat-new/server/internal/service"
)

// migrateTestEnabledConfig 已启用迁移功能的配置（不会真的连旧库，连接是懒建的）。
func migrateTestEnabledConfig() config.Config {
	return config.Config{
		LegacyAPISecret: "test-migrate-secret-0123456789abcdef",
		LegacyDBDSN:     "legacy_ro:pwd@tcp(127.0.0.1:3306)/chat?charset=utf8mb4&parseTime=True&loc=Local",
	}
}

// newMigrateTestContext 构造 gin 测试上下文（带可选请求头）。
func newMigrateTestContext(headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/migrate/preview", nil)
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return c, w
}

// TestMigrateGuardDisabled 迁移功能未启用时一律 503（契约：迁移功能未启用）。
func TestMigrateGuardDisabled(t *testing.T) {
	cfg := config.Config{} // 两个 env 都为空
	h := NewMigrateHandler(cfg, service.NewMigrateService(cfg, nil, nil, nil, nil))

	c, w := newMigrateTestContext(map[string]string{"X-Migrate-Secret": "whatever"})
	if h.guard(c) {
		t.Fatal("未启用时 guard 应返回 false")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用应返回 503，得到 %d", w.Code)
	}
	if got := w.Body.String(); got == "" || !containsAll(got, "迁移功能未启用") {
		t.Fatalf("503 响应体应包含「迁移功能未启用」，得到 %q", got)
	}
}

// TestMigrateGuardSecret 校验 X-Migrate-Secret：缺失/错误 401，正确放行。
func TestMigrateGuardSecret(t *testing.T) {
	cfg := migrateTestEnabledConfig()
	h := NewMigrateHandler(cfg, service.NewMigrateService(cfg, nil, nil, nil, nil))

	// 缺失请求头 → 401
	c, w := newMigrateTestContext(nil)
	if h.guard(c) {
		t.Fatal("缺失 X-Migrate-Secret 时 guard 应返回 false")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("缺失密钥应返回 401，得到 %d", w.Code)
	}

	// 错误请求头 → 401
	c, w = newMigrateTestContext(map[string]string{"X-Migrate-Secret": "wrong-secret"})
	if h.guard(c) {
		t.Fatal("错误 X-Migrate-Secret 时 guard 应返回 false")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("错误密钥应返回 401，得到 %d", w.Code)
	}

	// 正确请求头 → 放行
	c, w = newMigrateTestContext(map[string]string{"X-Migrate-Secret": cfg.LegacyAPISecret})
	if !h.guard(c) {
		t.Fatalf("正确密钥应放行，实际响应码 %d", w.Code)
	}
}

// TestExcludeIDList 排除列表解析：null/省略 = 不排除；正整数去重；非法值忽略。
func TestExcludeIDList(t *testing.T) {
	if got := excludeIDList(nil); got != nil {
		t.Fatalf("nil 排除列表应返回 nil，得到 %v", got)
	}
	empty := []int64{}
	if got := excludeIDList(&empty); len(got) != 0 {
		t.Fatalf("空数组应返回空切片，得到 %v", got)
	}
	ids := []int64{801, 0, -3, 801, 802}
	got := excludeIDList(&ids)
	want := []int64{801, 802}
	if len(got) != len(want) {
		t.Fatalf("排除列表去重结果 %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("排除列表去重结果 %v，期望 %v", got, want)
		}
	}
}

// containsAll 判断响应体是否包含指定子串（避免引 go 标准库之外的断言库）。
func containsAll(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

// indexOf 简单子串查找。
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
