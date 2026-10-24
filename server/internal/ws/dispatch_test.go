package ws

import (
	"testing"
)

func TestIsSafeMsgType(t *testing.T) {
	good := []string{"text", "weather", "image-v2", "a_b"}
	for _, s := range good {
		if !isSafeMsgType(s) {
			t.Errorf("%q should be safe", s)
		}
	}
	bad := []string{"", "With Upper", "emoji😀", "sql;drop"}
	for _, s := range bad {
		if isSafeMsgType(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
}

func TestUkey(t *testing.T) {
	if ukey(0) != "u0" {
		t.Fatalf("ukey(0) = %s", ukey(0))
	}
	if ukey(42) != "u42" {
		t.Fatalf("ukey(42) = %s", ukey(42))
	}
	// 稳定性
	if ukey(123456789) != ukey(123456789) {
		t.Fatal("ukey should be stable")
	}
}

func TestWithoutID(t *testing.T) {
	got := withoutID([]uint{1, 2, 3, 2}, 2)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("withoutID = %v", got)
	}
	if len(withoutID(nil, 1)) != 0 {
		t.Fatal("nil input should give empty")
	}
}

func TestHubOnlineAndToUsersNoPanic(t *testing.T) {
	hub := NewHub(nil)
	if hub.Online(1) {
		t.Fatal("empty hub should report offline")
	}
	// 不应 panic（无连接时 ToUsers 为 no-op）
	hub.ToUsers([]uint{1, 2}, ginH{"type": "ping"})
	client := &Client{UserID: 1, send: make(chan []byte, 1)}
	hub.Register(client)
	if !hub.Online(1) {
		t.Fatal("registered client should be online")
	}
	hub.ToUsers([]uint{1}, ginH{"type": "message", "id": 1})
	if len(client.send) != 1 {
		t.Fatal("frame should be queued")
	}
	hub.Unregister(client)
	if hub.Online(1) {
		t.Fatal("after unregister should be offline")
	}
}
