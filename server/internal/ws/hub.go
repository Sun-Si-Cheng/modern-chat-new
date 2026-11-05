package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

const ShardCount = 64

// kickGrace 踢线兜底延迟：writePump 正常时立即刷帧并关连接；
// 若 writePump 已退出（半死连接），靠该定时器保证连接一定被关闭。
const kickGrace = 2 * time.Second

type shard struct {
	mu    sync.RWMutex
	conns map[uint]map[*Client]struct{} // userID -> 该用户全部连接
}

// Deps Hub 依赖：stores + 消息限流器。
type Deps struct {
	Convs      *store.ConversationStore
	Messages   *store.MessageStore
	Friends    *store.FriendStore
	Users      *store.UserStore   // 连接级封禁 / token_version 复核（可为 nil）
	MsgLimiter middleware.Limiter // 每用户 20 条/分（内存或 Redis）
	// Storage 用于校验图片/语音消息的 URL 归属（可为 nil，测试环境走本地前缀兜底）
	Storage service.Storage
	// Settings 提供可运营的聊天规则（消息长度、撤回时限）。可为 nil，回落默认值。
	Settings *store.SettingStore
}

// Hub 在线连接注册表（按用户分片锁）。
type Hub struct {
	shards [ShardCount]*shard
	deps   *Deps
	done   chan struct{}
	closed bool
	mu     sync.RWMutex
}

// Client 一条 WS 连接。
type Client struct {
	UserID uint
	// tv JWT 中的 token_version（握手时已校验）；handleChat 用它做连接级复核。
	tv   int64
	Conn *websocket.Conn
	send chan []byte
	hub  *Hub

	// kickCh 关闭请求：由 KickUser / 慢消费者判定关闭（只关一次），
	// writePump 收到后发 WS 关闭帧退出。kickFlush=true 时先冲刷队列
	//（KickUser 需要把 kicked 帧送达），false 时立即退出（慢消费者）。
	// 注意：c.send 的关闭权仍只属于 Unregister，本结构绝不 close(c.send)。
	kickCh    chan struct{}
	kickFlush atomic.Bool
	kickOnce  sync.Once
	// pendingClose 慢消费者待关闭标记（atomic 保证同一连接只收集一次，
	// 避免 ToUsers 在锁内阻塞，也避免重复触发关闭）。
	pendingClose atomic.Bool
	// sendOnce 保证 c.send 只被 close 一次（Unregister 幂等保护）。
	sendOnce sync.Once
}

func newShard() *shard { return &shard{conns: make(map[uint]map[*Client]struct{})} }

// NewHub 创建 Hub（deps 可为 nil：仅注册表功能，供测试）。
func NewHub(deps *Deps) *Hub {
	h := &Hub{deps: deps, done: make(chan struct{})}
	for i := range h.shards {
		h.shards[i] = newShard()
	}
	return h
}

// Run 阻塞至 Close（预留统一事件循环扩展位）。
func (h *Hub) Run() {
	<-h.done
}

// Register 连接上线。
func (h *Hub) Register(c *Client) {
	s := h.shards[c.UserID%ShardCount]
	s.mu.Lock()
	if s.conns[c.UserID] == nil {
		s.conns[c.UserID] = make(map[*Client]struct{})
	}
	s.conns[c.UserID][c] = struct{}{}
	s.mu.Unlock()
	slog.Debug("ws register", "uid", c.UserID)
}

// Unregister 连接下线（引用计数：最后一条连接才移除用户）。
//
// 修复缺陷：c.send 的关闭权唯一属于本函数，且必须幂等 —— 被 KickUser /
// 慢消费者踢掉的连接也会走到这里（readPump 退出触发），若关闭两次会 panic。
// 因此统一用 sendOnce 保护；另在 Hub.Close 清空注册表后仍要关闭 send，
// 否则 writePump 无信号退出会泄漏 goroutine。
func (h *Hub) Unregister(c *Client) {
	s := h.shards[c.UserID%ShardCount]
	s.mu.Lock()
	if set, ok := s.conns[c.UserID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(s.conns, c.UserID)
		}
	}
	// 持写锁期间关闭：ToUsers 只在读锁内投递，二者互斥；且连接已从
	// 注册表摘除，不会再有新的投递落到这个已关闭的 channel 上。
	c.sendOnce.Do(func() { close(c.send) })
	s.mu.Unlock()
}

// KickUser 强制断开某用户的全部既有连接（封禁 / 重置密码后 tv 已失效）。
//
// 修复缺陷：此前 admin 仅推 kicked 帧而不关连接，被封禁用户忽略该帧即可
// 继续发消息、写库与 fan-out；握手段校验只管新连接。本方法在分片锁内
// 「先入帧、后关连接」，且严格不 close(c.send)（所有权只属 Unregister）：
//   - trySend 非阻塞入帧，锁内绝不等待网络写 / channel；
//   - 锁外通过 kickCh 通知 writePump 冲刷队列、发 WS 关闭帧后自行退出；
//   - 另有 kickGrace 兜底定时器强制 Close，防止 writePump 已死时连接残留。
func (h *Hub) KickUser(userID uint, reason string) {
	data, err := json.Marshal(ginH{"type": "kicked", "reason": reason})
	if err != nil {
		slog.Error("ws marshal kicked frame", "err", err.Error())
		return
	}
	if userID == 0 {
		return
	}
	s := h.shards[userID%ShardCount]
	s.mu.Lock()
	targets := make([]*Client, 0, len(s.conns[userID]))
	for c := range s.conns[userID] {
		c.trySend(data) // 非阻塞：慢消费者也不阻塞踢线流程
		targets = append(targets, c)
	}
	s.mu.Unlock()

	// 锁外关连接：避免在持有分片锁时做任何可能阻塞的操作。
	for _, c := range targets {
		c.requestKick(true) // 先冲刷队列，保证 kicked 帧送达
	}
	if len(targets) > 0 {
		slog.Info("ws kicked user conns", "uid", userID, "conns", len(targets))
	}
}

// requestKick 请求断开连接：flush=true 时通知 writePump 先冲刷队列
// （KickUser 需送达 kicked 帧），flush=false 时立即关闭（慢消费者）。
// 幂等：同一条连接被多次判定关闭也只触发一次。
func (c *Client) requestKick(flush bool) {
	c.kickOnce.Do(func() {
		if c.kickCh == nil {
			// 测试 / 非常规构造的连接：没有 writePump，直接关底层连接。
			if c.Conn != nil {
				_ = c.Conn.Close()
			}
			return
		}
		// 必须先写 kickFlush 再 close kickCh：writePump 收到信号时读取该标记，
		// close(channel) 与 atomic 写之间由 channel 操作建立 happens-before。
		c.kickFlush.Store(flush)
		close(c.kickCh)
		// 兜底：writePump 若已退出或卡在网络写，kickGrace 后强制断开。
		time.AfterFunc(kickGrace, func() {
			if c.Conn != nil {
				_ = c.Conn.Close()
			}
		})
	})
}

// Online 用户是否有在线连接。
func (h *Hub) Online(userID uint) bool {
	s := h.shards[userID%ShardCount]
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns[userID]) > 0
}

// ToUsers 向一组在线用户的全部连接推帧（序列化一次）。
//
// 修复缺陷：此前 send channel 满时走 default 分支静默丢帧 —— 客户端既收不到
// 该帧、也不会被断开，表现为「消息丢了但连接还活着」。现在改为：标记慢消费者
// pendingClose，把该连接收集到待关闭列表，**锁外**统一触发关闭。锁内只做
// 非阻塞投递与收集，绝不在分片锁内 close / 网络写 / 等待。
func (h *Hub) ToUsers(userIDs []uint, frame any) {
	data, err := json.Marshal(frame)
	if err != nil {
		slog.Error("ws marshal frame", "err", err.Error())
		return
	}
	// slow 收集跨分片发现的慢消费者：锁外关闭，避免在 RLock 内 close。
	var slow []*Client
	for _, uid := range userIDs {
		s := h.shards[uid%ShardCount]
		s.mu.RLock()
		for c := range s.conns[uid] {
			select {
			case c.send <- data:
			default:
				// 慢消费者：标记待关闭（CAS 保证只收集一次），锁内不做阻塞操作
				if c.pendingClose.CompareAndSwap(false, true) {
					slow = append(slow, c)
					slog.Warn("ws slow consumer, closing conn", "uid", c.UserID)
				}
			}
		}
		s.mu.RUnlock()
	}
	// 锁外关闭：RLock 已释放，requestKick 只 close 信号 channel + 设兜底定时器。
	for _, c := range slow {
		c.requestKick(false)
	}
}

// Close 关闭全部连接并退出 Run。
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	close(h.done)
	h.mu.Unlock()
	// 锁内只收集，锁外关闭：避免持分片锁做网络操作。
	var all []*Client
	for i := range h.shards {
		s := h.shards[i]
		s.mu.Lock()
		for uid, set := range s.conns {
			for c := range set {
				all = append(all, c)
			}
			delete(s.conns, uid)
		}
		s.mu.Unlock()
	}
	for _, c := range all {
		// 只关底层连接：readPump 的 ReadMessage 随即出错退出，由它的 defer
		// 调用 Unregister 关闭 send（关闭权唯一）并唤醒 writePump。
		// 这里绝不能自行 close(c.send)：Dispatch 的 sendError 走 trySend 且
		// 不持分片锁，并发 close 会触发 send on closed channel panic。
		_ = c.Conn.Close()
	}
}

// ---- 帧发送小工具 ----

func (c *Client) trySend(data []byte) {
	select {
	case c.send <- data:
	default:
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// ginH 帧字面量类型（避免本包依赖 gin）。
type ginH = map[string]any

// OnlineCount 当前在线用户数（同一用户多端只计一次）。
func (h *Hub) OnlineCount() int {
	n := 0
	for i := 0; i < ShardCount; i++ {
		s := h.shards[i]
		s.mu.RLock()
		n += len(s.conns)
		s.mu.RUnlock()
	}
	return n
}
