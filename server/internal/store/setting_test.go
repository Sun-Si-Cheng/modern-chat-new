package store

import "testing"

// 注册邮箱验证的默认值必须是「开」，但只有显式 "false" 才关闭：
// 两者口径不同，写反了会让升级后的站点突然放开验证（安全回退）。
func TestOpsFromRequireEmailVerify(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]string
		want bool
	}{
		{"未配置默认开", map[string]string{}, true},
		{"显式开", map[string]string{KeySiteRequireEmailVerify: "true"}, true},
		{"显式关", map[string]string{KeySiteRequireEmailVerify: "false"}, false},
		{"脏值保持默认开", map[string]string{KeySiteRequireEmailVerify: "0"}, true},
		{"空串保持默认开", map[string]string{KeySiteRequireEmailVerify: "  "}, true},
	}
	for _, c := range cases {
		if got := OpsFrom(c.raw).RequireEmailVerify; got != c.want {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// 邀请码开关方向相反：默认关、只有显式 "true" 才打开。
func TestOpsFromRequireInviteCode(t *testing.T) {
	if OpsFrom(map[string]string{}).RequireInviteCode {
		t.Fatal("未配置时邀请码应为关闭")
	}
	if !OpsFrom(map[string]string{KeySiteRequireInviteCode: "true"}).RequireInviteCode {
		t.Fatal("显式 true 时应开启")
	}
}
