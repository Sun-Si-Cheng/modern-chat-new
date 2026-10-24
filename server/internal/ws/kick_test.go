package ws

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// modelUserForTest notifyPresence 用的最小 User 实例。
var modelUserForTest = model.User{ID: 1}

// newTestClient 构造无底层连接的测试 Client（kickCh 存在，模拟真实连接）。
func newTestClient(hub *Hub, uid uint, buf int) *Client {
	return &Client{
		UserID: uid,
		send:   make(chan []byte, buf),
		kickCh: make(chan struct{}),
		hub:    hub,
	}
}

// TestKickUserQueuesFrameAndSignals 校验 KickUser 的核心语义：
// 1) kicked 帧进入 c.send（非阻塞）；2) kickCh 被关闭以通知 writePump；
// 3) 不 close(c.send) —— 所有权仍属 Unregister（重复 Unregister 不 panic）。
func TestKickUserQueuesFrameAndSignals(t *testing.T) {
	hub := NewHub(nil)
	c := newTestClient(hub, 7, 4)
	hub.Register(c)

	hub.KickUser(7, "账号已被封禁")

	// 1) kicked 帧已入队
	select {
	case data := <-c.send:
		var f map[string]any
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("kicked 帧不是合法 JSON: %v", err)
		}
		if f["type"] != "kicked" || f["reason"] != "账号已被封禁" {
			t.Fatalf("kicked 帧内容异常: %v", f)
		}
	case <-time.After(time.Second):
		t.Fatal("KickUser 应把 kicked 帧入队")
	}

	// 2) kickCh 已关闭（writePump 据此退出）
	select {
	case <-c.kickCh:
	case <-time.After(time.Second):
		t.Fatal("KickUser 应关闭 kickCh 通知 writePump")
	}
	if !c.kickFlush.Load() {
		t.Fatal("KickUser 应设置 kickFlush=true 以冲刷队列")
	}

	// 3) c.send 可写未被关闭（仍由 Unregister 关闭）
	c.trySend([]byte(`{"type":"x"}`))

	// 4) Unregister 幂等：重复调用不 panic（sendOnce 保护）
	hub.Unregister(c)
	hub.Unregister(c)
	if hub.Online(7) {
		t.Fatal("unregister 后应离线")
	}
}

// TestKickUserUnknownUserNoPanic 无连接用户踢线应安全 no-op。
func TestKickUserUnknownUserNoPanic(t *testing.T) {
	hub := NewHub(nil)
	hub.KickUser(999, "x")
	hub.KickUser(0, "x")
	if hub.Online(999) {
		t.Fatal("不应因 KickUser 产生连接")
	}
}

// TestKickUserAllConnsOfUser 多端登录时踢掉该用户全部连接。
func TestKickUserAllConnsOfUser(t *testing.T) {
	hub := NewHub(nil)
	a := newTestClient(hub, 42, 2)
	b := newTestClient(hub, 42, 2)
	other := newTestClient(hub, 43, 2)
	hub.Register(a)
	hub.Register(b)
	hub.Register(other)

	hub.KickUser(42, "重置")

	for i, c := range []*Client{a, b} {
		select {
		case <-c.kickCh:
		case <-time.After(time.Second):
			t.Fatalf("第 %d 条连接未被通知关闭", i+1)
		}
	}
	select {
	case <-other.kickCh:
		t.Fatal("其他用户不应被踢")
	default:
	}
}

// TestUnregisterClosesSendOnce 校验 Unregister 与 KickUser 并发下不重复 close。
func TestUnregisterClosesSendOnce(t *testing.T) {
	hub := NewHub(nil)
	c := newTestClient(hub, 5, 1)
	hub.Register(c)

	var wg sync.WaitGroup
	// 并发：一边 KickUser（会 close kickCh）、一边 Unregister（会 close send）
	wg.Add(2)
	go func() { defer wg.Done(); hub.KickUser(5, "并发") }()
	go func() { defer wg.Done(); hub.Unregister(c) }()
	wg.Wait()
	// KickUser 之后 readPump 还会再 Unregister 一次：必须不 panic
	hub.Unregister(c)
	// 注意：send 被关闭时可能仍残留 KickUser 塞入的缓冲帧，
	// 对已关闭的带缓冲 channel 读取会先返回缓冲元素且 ok=true，
	// 因此必须排空后才能确认已关闭。
	closed := false
	for i := 0; i <= 4; i++ {
		if _, ok := <-c.send; !ok {
			closed = true
			break
		}
	}
	if !closed {
		t.Fatal("Unregister 后 send 应已关闭")
	}
}

// TestToUsersSlowConsumerMarkedForClose 校验慢消费者不再静默丢帧，
// 而是被标记 pendingClose 并触发关闭信号（锁外执行）。
func TestToUsersSlowConsumerMarkedForClose(t *testing.T) {
	hub := NewHub(nil)
	c := newTestClient(hub, 9, 1) // 缓冲区 1，第二帧必然满
	hub.Register(c)

	// 先占满缓冲区
	hub.ToUsers([]uint{9}, ginH{"type": "fill"})
	if len(c.send) != 1 {
		t.Fatalf("缓冲区应已占满，len=%d", len(c.send))
	}

	// 再推一帧：应判定为慢消费者并请求关闭，而不是静默丢弃
	hub.ToUsers([]uint{9}, ginH{"type": "overflow"})

	if !c.pendingClose.Load() {
		t.Fatal("send 满时应把连接标记为 pendingClose")
	}
	select {
	case <-c.kickCh:
	case <-time.After(time.Second):
		t.Fatal("慢消费者应被请求关闭（kickCh 关闭）")
	}
	if c.kickFlush.Load() {
		t.Fatal("慢消费者关闭不应冲刷队列（kickFlush 应为 false）")
	}
	hub.Unregister(c)
}

// TestToUsersFastConsumerNotClosed 正常消费者不应被标记关闭。
func TestToUsersFastConsumerNotClosed(t *testing.T) {
	hub := NewHub(nil)
	c := newTestClient(hub, 11, 8)
	hub.Register(c)

	hub.ToUsers([]uint{11}, ginH{"type": "ok"})
	if c.pendingClose.Load() {
		t.Fatal("正常消费者不应被标记关闭")
	}
	select {
	case <-c.kickCh:
		t.Fatal("正常消费者不应被请求关闭")
	default:
	}
	if len(c.send) != 1 {
		t.Fatalf("帧应入队，len=%d", len(c.send))
	}
	hub.Unregister(c)
}

// TestPresenceKeepsOnlineWhileOtherConn 校验多端 presence 引用计数：
// 用户仍有其它连接在线时，Unregister 后的 Online 判断应为 true（不广播 offline）。
func TestPresenceKeepsOnlineWhileOtherConn(t *testing.T) {
	hub := NewHub(nil)
	conn1 := newTestClient(hub, 3, 4)
	conn2 := newTestClient(hub, 3, 4)
	hub.Register(conn1)
	hub.Register(conn2)

	// 断开第一端
	hub.Unregister(conn1)
	if !hub.Online(3) {
		t.Fatal("仍有另一条连接在线时用户应保持 online（否则会误报离线）")
	}

	// 断开最后一条连接
	hub.Unregister(conn2)
	if hub.Online(3) {
		t.Fatal("最后一条连接断开后应离线")
	}
}

// TestRelatedUsersNilDepsNoPanic 校验 deps 字段为 nil 时不 panic（单测/未完整注入场景）。
func TestRelatedUsersNilDepsNoPanic(t *testing.T) {
	hub := NewHub(&Deps{}) // Convs / Friends 全为 nil
	got := hub.relatedUsers(1)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("应只包含自己，got %v", got)
	}
	// notifyPresence 走 ToUsers，deps 非 nil 但字段 nil，不应 panic
	hub.notifyPresence(&modelUserForTest, true)
}
