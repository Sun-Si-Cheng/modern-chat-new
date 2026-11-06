package ws

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

const (
	writeWait  = 10 * time.Second // 单次写超时
	pongWait   = 60 * time.Second // 读超时（客户端 25s 心跳，服务端协议 ping 兜底）
	pingPeriod = 50 * time.Second
	sendBuffer = 256
	// maxMsgSize 上行帧上限（字节）。必须大于「后台可配的最大消息长度」的字节数：
	// chat.maxMessageLength 上限为 5000 个字符，UTF-8 下最多约 20 KB（4 字节/字符），
	// 再加上 clientMsgId、引用、mentions 等 JSON 开销。原来固定 4096 字节时，
	// 管理员把长度调到几千后，合法长消息会触发 gorilla 的 read limit 直接断连，
	// 用户连 error 帧都收不到。64 KB 留足余量，不做无上限放大。
	maxMsgSize = 64 * 1024
)

// upgraderFor 构造带 Origin 白名单的升级器：
// 浏览器 WS 升级必带 Origin，非白名单来源拒绝（防跨站 WebSocket 劫持）；
// 无 Origin（curl / 服务端客户端）放行，仍受 token 鉴权保护。
func upgraderFor(allowed []string) websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		// permessage-deflate（设计文档 §9.2）：浏览器默认请求该扩展，
		// 开启后文本帧自动压缩；连接数压顶时可置 false 换取 CPU。
		EnableCompression: true,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			origin = strings.TrimSuffix(strings.ToLower(origin), "/")
			for _, a := range allowed {
				if origin == strings.ToLower(a) {
					return true
				}
			}
			slog.Warn("ws origin rejected", "origin", origin, "remote", r.RemoteAddr)
			return false
		},
	}
}

// Serve 握手 + 鉴权 + 启动读写 pump。
func Serve(
	secret string,
	allowedOrigins []string,
	users *store.UserStore,
	hub *Hub,
	settings *store.SettingStore,
	w http.ResponseWriter,
	r *http.Request,
) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, `{"error":"登录已失效，请重新登录"}`, http.StatusUnauthorized)
		return
	}
	cl, err := middleware.ParseToken(token, secret)
	if err != nil {
		slog.Warn("ws handshake rejected: bad token", "remote", r.RemoteAddr)
		http.Error(w, `{"error":"登录已失效，请重新登录"}`, http.StatusUnauthorized)
		return
	}
	u, err := users.ByID(cl.UID)
	if err != nil || u == nil || u.TokenVersion != cl.TV {
		slog.Warn("ws handshake rejected: stale token", "uid", cl.UID, "remote", r.RemoteAddr)
		http.Error(w, `{"error":"登录已失效，请重新登录"}`, http.StatusUnauthorized)
		return
	}
	if u.IsBanned() {
		slog.Warn("ws handshake rejected: banned", "uid", u.ID, "remote", r.RemoteAddr)
		http.Error(w, `{"error":"账号已被封禁"}`, http.StatusForbidden)
		return
	}
	// 维护模式下非管理员不许建连：REST 已经被中间件挡住，
	// 这里不挡的话页面虽然显示维护页，已有连接仍能继续收发消息
	if settings != nil && settings.Ops().MaintenanceMode && u.Role != "admin" {
		http.Error(w, `{"error":"站点维护中","maintenance":true}`, http.StatusServiceUnavailable)
		return
	}

	up := upgraderFor(allowedOrigins)
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &Client{
		UserID: u.ID,
		tv:     cl.TV, // 缓存握手时的 token_version，供 handleChat 连接级复核
		Conn:   conn,
		send:   make(chan []byte, sendBuffer),
		kickCh: make(chan struct{}),
		hub:    hub,
	}
	hub.Register(client)
	slog.Info("ws connected", "uid", u.ID)

	// 上线 presence（好友 + 同会话成员）
	go hub.notifyPresence(u, true)

	go writePump(client)
	readPump(client)
}

// writePump 单连接写循环：send channel + 协议层 ping + 踢线信号；写满即踢（慢消费者）。
//
// 修复缺陷：收到 kickCh 信号（KickUser 下发 kicked 帧后触发）时，先把
// send 队列里残留的帧冲刷掉（保证 kicked 帧送达），再发 WS CloseMessage
// 并退出；c.send 的关闭仍由 readPump defer 里的 Unregister 独占。
func writePump(c *Client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.Conn.Close()
	}()
	for {
		select {
		case data, ok := <-c.send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(data); err != nil {
				return
			}
			if err := w.Close(); err != nil {
				return
			}
		case <-c.kickCh:
			// KickUser 场景（kickFlush=true）先冲刷队列，保证 kicked 帧送达；
			// 慢消费者场景（kickFlush=false）直接退出，避免为已判定超载的连接
			// 继续占用写带宽。
			if c.kickFlush.Load() {
				for i := len(c.send); i > 0; i-- {
					data, ok := <-c.send
					if !ok {
						break
					}
					_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
					if err := c.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
						break
					}
				}
			}
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			_ = c.Conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "kicked"))
			return
		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump 单连接读循环：每帧刷新读超时，帧交给 Hub 分发。
func readPump(c *Client) {
	defer func() {
		// 修复缺陷：先把连接从注册表摘除，再判断是否还有其它连接在线。
		// 原实现无条件广播 offline，导致「手机 + 网页」多端登录时，任一端
		// 断开就让好友看到该用户离线（误报）；现在仅当该用户已无任何在线
		// 连接时才广播 offline，单端在线期间保持 online 状态。
		c.hub.Unregister(c)
		if !c.hub.Online(c.UserID) {
			go c.hub.notifyPresence(&model.User{ID: c.UserID}, false)
		} else {
			slog.Debug("ws conn closed, user still online", "uid", c.UserID)
		}
		_ = c.Conn.Close()
		slog.Info("ws disconnected", "uid", c.UserID)
	}()
	c.Conn.SetReadLimit(maxMsgSize)
	_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		return c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, data, err := c.Conn.ReadMessage()
		if err != nil {
			return
		}
		_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		c.hub.Dispatch(c, data)
	}
}
