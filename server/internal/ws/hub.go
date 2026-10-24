package ws

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/gorilla/websocket"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

const ShardCount = 64

type shard struct {
	mu    sync.RWMutex
	conns map[uint]map[*Client]struct{} // userID -> 该用户全部连接
}

// Deps Hub 依赖：stores + 消息限流器。
type Deps struct {
	Convs      *store.ConversationStore
	Messages   *store.MessageStore
	Friends    *store.FriendStore
	MsgLimiter middleware.Limiter // 每用户 20 条/分（内存或 Redis）
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
	Conn   *websocket.Conn
	send   chan []byte
	hub    *Hub
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
func (h *Hub) Unregister(c *Client) {
	s := h.shards[c.UserID%ShardCount]
	s.mu.Lock()
	if set, ok := s.conns[c.UserID]; ok {
		if _, ok := set[c]; ok {
			delete(set, c)
			close(c.send)
		}
		if len(set) == 0 {
			delete(s.conns, c.UserID)
		}
	}
	s.mu.Unlock()
}

// Online 用户是否有在线连接。
func (h *Hub) Online(userID uint) bool {
	s := h.shards[userID%ShardCount]
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns[userID]) > 0
}

// ToUsers 向一组在线用户的全部连接推帧（序列化一次）。
func (h *Hub) ToUsers(userIDs []uint, frame any) {
	data, err := json.Marshal(frame)
	if err != nil {
		slog.Error("ws marshal frame", "err", err.Error())
		return
	}
	for _, uid := range userIDs {
		s := h.shards[uid%ShardCount]
		s.mu.RLock()
		for c := range s.conns[uid] {
			select {
			case c.send <- data:
			default:
				// 慢消费者：由写超时兜底断开
			}
		}
		s.mu.RUnlock()
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
	for i := range h.shards {
		s := h.shards[i]
		s.mu.Lock()
		for uid, set := range s.conns {
			for c := range set {
				_ = c.Conn.Close()
			}
			delete(s.conns, uid)
		}
		s.mu.Unlock()
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
