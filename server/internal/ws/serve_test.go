package ws

import (
	"net/http"
	"testing"
)

func TestUpgraderCheckOrigin(t *testing.T) {
	allowed := []string{"http://localhost:5173", "https://chat.example.com"}
	up := upgraderFor(allowed)

	cases := []struct {
		origin string
		want   bool
	}{
		{"", true},                                // 非浏览器客户端：放行（token 保护）
		{"http://localhost:5173", true},           // 白名单命中
		{"http://localhost:5173/", true},          // 尾斜杠归一化
		{"HTTPS://CHAT.EXAMPLE.COM", true},        // 大小写归一化
		{"http://evil.example.com", false},        // 非白名单拒绝
		{"http://localhost:5173.evil.com", false}, // 前缀伪装拒绝
		{"http://localhost:5174", false},          // 端口不同拒绝
	}
	for _, tc := range cases {
		r, _ := http.NewRequest(http.MethodGet, "/", nil)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		if got := up.CheckOrigin(r); got != tc.want {
			t.Errorf("Origin %q: expect %v, got %v", tc.origin, tc.want, got)
		}
	}
}
