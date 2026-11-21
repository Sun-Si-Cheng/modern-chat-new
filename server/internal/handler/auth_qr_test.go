package handler

import (
	"testing"
	"time"
)

// 扫码登录会话状态机回归：状态流转（待确认→已确认→已消费）与过期/未知票据。
func TestQRSessionFlow(t *testing.T) {
	token, expires, err := qrIssueSession()
	if err != nil {
		t.Fatal(err)
	}
	if expires.IsZero() || !expires.After(time.Now()) {
		t.Fatalf("expires 异常: %v", expires)
	}

	if state, uid := qrTakeSession(token); state != 0 {
		t.Fatalf("未确认时应为 0，got %d (uid=%d)", state, uid)
	}

	if !qrConfirmSession(token, 42) {
		t.Fatal("确认应成功")
	}
	if qrConfirmSession(token, 43) {
		t.Fatal("已确认的会话不能被二次确认（换人抢占）")
	}

	state, uid := qrTakeSession(token)
	if state != 1 || uid != 42 {
		t.Fatalf("确认后应取走登录态 (1,42)，got (%d,%d)", state, uid)
	}
	// 一次性消费：第二个轮询拿不到
	if state, _ := qrTakeSession(token); state != 2 {
		t.Fatalf("已消费后应为 2，got %d", state)
	}
}

func TestQRSessionUnknownToken(t *testing.T) {
	if state, _ := qrTakeSession("no-such-token"); state != 2 {
		t.Fatalf("未知票据应为 2，got %d", state)
	}
	if qrConfirmSession("no-such-token", 1) {
		t.Fatal("未知票据确认应失败")
	}
}

func TestQRSessionExpired(t *testing.T) {
	token, _, err := qrIssueSession()
	if err != nil {
		t.Fatal(err)
	}
	// 直接把会话改过期（同包测试，允许操作内部状态）
	qrSessions.Lock()
	qrSessions.m[token].expires = time.Now().Add(-time.Minute)
	qrSessions.Unlock()

	if state, _ := qrTakeSession(token); state != 2 {
		t.Fatalf("过期会话应为 2，got %d", state)
	}
	if qrConfirmSession(token, 1) {
		t.Fatal("过期会话确认应失败")
	}
}

func TestQRConfirmThenExpire(t *testing.T) {
	token, _, _ := qrIssueSession()
	if !qrConfirmSession(token, 7) {
		t.Fatal("确认应成功")
	}
	qrSessions.Lock()
	qrSessions.m[token].expires = time.Now().Add(-time.Minute)
	qrSessions.Unlock()
	// 已确认但过期：桌面轮询同样拿不到（防止过期窗口内换走登录态）
	if state, _ := qrTakeSession(token); state != 2 {
		t.Fatalf("已确认但过期应为 2，got %d", state)
	}
}
