package service

import "testing"

// endpoint 由客户端提交，服务端会向它 POST；必须只允许已知推送网关，
// 否则可被填成内网地址当探测代理，或任意 http 地址。
func TestValidPushEndpoint(t *testing.T) {
	ok := []string{
		"https://fcm.googleapis.com/fcm/send/abc",
		"https://updates.push.services.mozilla.com/wpush/v2/abc",
		"https://wns2-bl2p.notify.windows.com/w/?token=abc",
		"https://webpush.apple.com/push/abc",
		"https://xyz.push.apple.com/abc",
	}
	for _, u := range ok {
		if !ValidPushEndpoint(u) {
			t.Errorf("%q should be allowed", u)
		}
	}
	bad := []string{
		"http://fcm.googleapis.com/fcm/send/abc", // 非 https
		"https://evil.example.com/push",          // 非白名单域名
		"https://fcm.googleapis.com.evil.com/x",  // 后缀伪装
		"https://user:pass@fcm.googleapis.com/x", // userinfo
		"https://127.0.0.1/push",                 // 内网
		"https://notify-windows.com/push",        // 相似但不匹配
		"",                                       // 空
	}
	for _, u := range bad {
		if ValidPushEndpoint(u) {
			t.Errorf("%q should be rejected", u)
		}
	}
}
