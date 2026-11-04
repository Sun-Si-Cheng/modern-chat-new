package ws

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// 上行帧（§7）：宽松解析，按 type 路由。
type clientFrame struct {
	Type        string `json:"type"`
	ConvID      uint   `json:"convId"`
	ClientMsgID string `json:"clientMsgId"`
	Content     string `json:"content"`
	MsgType     string `json:"msgType"`
	UpToID      uint64 `json:"upToId"`
	MessageID   uint64 `json:"messageId"`
}

// Dispatch 上行帧路由：非成员一律 error 帧。
func (h *Hub) Dispatch(c *Client, data []byte) {
	var f clientFrame
	if err := json.Unmarshal(data, &f); err != nil {
		h.sendError(c, "帧格式错误")
		return
	}
	switch f.Type {
	case "ping":
		c.trySend(mustJSON(ginH{"type": "pong"}))
	case "chat":
		h.handleChat(c, &f)
	case "typing":
		h.handleTyping(c, &f)
	case "read":
		h.handleRead(c, &f)
	case "recall":
		h.handleRecall(c, &f)
	default:
		h.sendError(c, "未知帧类型")
	}
}

// handleChat 发消息链路（§9.1）。
func (h *Hub) handleChat(c *Client, f *clientFrame) {
	if h.deps == nil {
		return
	}
	// 修复缺陷：握手时校验过封禁/TokenVersion，但连接建立后 admin 封禁或
	// 重置密码（tv+1）不会作用于既有连接 —— 被处理过的用户只要忽略 kicked
	// 帧就能继续发消息、写库与 fan-out。这里在落库前做一次连接级复核：
	// 任一不通过则回 error 帧并 requestKick() 断开，与 REST 侧 401 行为一致。
	if !h.checkConnState(c, f.ConvID) {
		return
	}
	// 限流：每用户 20 条/分
	if !h.deps.MsgLimiter.Allow(ukey(c.UserID)) {
		slog.Warn("ws message rate limited", "uid", c.UserID, "conv", f.ConvID)
		h.sendError(c, "操作过于频繁，请稍后再试")
		return
	}
	if !h.requireMember(c, f.ConvID) {
		return
	}
	content := []rune(f.Content)
	if len(content) < 1 || len(content) > 1000 {
		h.sendError(c, "消息需为 1-1000 个字符")
		return
	}
	if f.ClientMsgID == "" || len(f.ClientMsgID) > 36 {
		h.sendError(c, "clientMsgId 不合法")
		return
	}
	msgType := f.MsgType
	if msgType == "" {
		msgType = "text"
	}
	if l := len(msgType); l > 32 || !isSafeMsgType(msgType) {
		h.sendError(c, "msgType 不合法")
		return
	}
	// 图片/语音的 content 必须是本站上传目录下的相对地址。
	// 不校验的话用户可以塞 https://evil.com/x.png，接收方客户端会去加载外部资源
	// （泄露 IP、可被打点统计），而消息看起来完全正常。
	if msgType == "image" || msgType == "audio" {
		// 由存储实现自己判断 URL 归属（本地是 /uploads/，S3 是配置的对外域名），
		// 这样换存储不用改校验逻辑。测试里未注入 Storage 时退回本地前缀判断。
		var ok bool
		if h.deps.Storage != nil {
			ok = h.deps.Storage.Owns(string(content))
		} else {
			ok = strings.HasPrefix(string(content), "/uploads/") && !strings.Contains(string(content), "..")
		}
		if !ok {
			h.sendError(c, "图片/语音消息的内容必须是本站上传地址")
			return
		}
	}

	// 幂等：同 client_msg_id 直接回放原消息
	if existing, err := h.deps.Messages.ByClientMsg(f.ConvID, f.ClientMsgID); err != nil {
		slog.Error("ws idempotent lookup", "err", err.Error())
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	} else if existing != nil {
		c.trySend(mustJSON(messageFrame(existing, f.ClientMsgID)))
		return
	}

	msg := &model.Message{
		ConversationID: f.ConvID,
		SenderID:       c.UserID,
		ClientMsgID:    f.ClientMsgID,
		MsgType:        msgType,
		Content:        string(content),
		CreatedAt:      time.Now(),
	}
	if err := h.deps.Messages.Create(msg); err != nil {
		slog.Error("ws create message", "err", err.Error(), "uid", c.UserID, "conv", f.ConvID)
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}
	// 消息正文不入日志（隐私），只记元数据
	slog.Debug("message stored",
		"msg", msg.ID, "conv", msg.ConversationID, "from", msg.SenderID,
		"type", msg.MsgType, "chars", len([]rune(msg.Content)))

	// 收件箱冗余：全员 last=新消息；新成员行在此首次创建
	memberIDs, err := h.deps.Convs.MemberUserIDs(f.ConvID)
	if err != nil {
		slog.Error("ws members", "err", err.Error())
		return
	}
	rows := make([]model.UserConversation, 0, len(memberIDs))
	for _, uid := range memberIDs {
		rows = append(rows, model.UserConversation{
			UserID:         uid,
			ConversationID: f.ConvID,
			LastMessageID:  msgIDPtr(uint64(msg.ID)),
			LastMessageAt:  &msg.CreatedAt,
		})
	}
	if err := h.deps.Messages.UpsertInbox(rows); err != nil {
		slog.Error("ws upsert inbox", "err", err.Error())
	}
	if err := h.deps.Messages.BumpUnread(f.ConvID, c.UserID, uint64(msg.ID)); err != nil {
		slog.Error("ws bump unread", "err", err.Error())
	}

	// fan-out：message 帧；非发送者额外 conv_change
	mf := messageFrame(msg, f.ClientMsgID)
	others := withoutID(memberIDs, c.UserID)
	h.ToUsers(memberIDs, mf)
	h.ToUsers(others, ginH{"type": "conv_change", "convId": f.ConvID})
}

// handleTyping 转发正在输入（不落库）。
func (h *Hub) handleTyping(c *Client, f *clientFrame) {
	if !h.requireMember(c, f.ConvID) {
		return
	}
	ids, err := h.deps.Convs.MemberUserIDs(f.ConvID)
	if err != nil {
		return
	}
	h.ToUsers(withoutID(ids, c.UserID), ginH{"type": "typing", "convId": f.ConvID, "userId": c.UserID})
}

// handleRead 已读上报。
//
// 修复缺陷：此前只校验了 upToId != 0 与成员身份，未校验该消息是否属于本会话，
// 客户端上报 9e18 之类的伪造 ID 即可把 last_read_message_id 永久推到未来，
// 从而废掉撤回时的未读回退（RecallUnreadAdjust 以 last_read_message_id < msgID
// 为条件），并伪造「已读到最新」。现在落库前先按 id 反查消息，要求存在且
// ConversationID 与上报的 convId 一致，否则回 error 帧并丢弃。
func (h *Hub) handleRead(c *Client, f *clientFrame) {
	if f.UpToID == 0 || !h.requireMember(c, f.ConvID) {
		return
	}
	// upToId 归属校验：必须真实存在且属于本会话（防止伪造超大 ID 污染已读游标）。
	if h.deps == nil || h.deps.Messages == nil {
		return
	}
	msg, err := h.deps.Messages.ByID(uint(f.UpToID))
	if err != nil {
		slog.Error("ws read lookup", "err", err.Error(), "uid", c.UserID, "conv", f.ConvID)
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}
	if msg == nil || msg.ConversationID != f.ConvID {
		slog.Warn("ws read rejected: upToId not in conversation",
			"uid", c.UserID, "conv", f.ConvID, "upToId", f.UpToID)
		h.sendError(c, "已读位置不合法")
		return
	}
	affected, err := h.deps.Messages.MarkRead(c.UserID, f.ConvID, f.UpToID)
	if err != nil {
		slog.Error("ws mark read", "err", err.Error())
		return
	}
	if affected == 0 {
		return
	}
	ids, err := h.deps.Convs.MemberUserIDs(f.ConvID)
	if err != nil {
		return
	}
	h.ToUsers(withoutID(ids, c.UserID), ginH{
		"type": "read", "convId": f.ConvID, "userId": c.UserID, "upToId": f.UpToID,
	})
}

// handleRecall 撤回：本人 + 2 分钟内；未读接收者回退。
func (h *Hub) handleRecall(c *Client, f *clientFrame) {
	if f.MessageID == 0 {
		return
	}
	msg, err := h.deps.Messages.ByID(uint(f.MessageID))
	if err != nil || msg == nil || msg.SenderID != c.UserID {
		return
	}
	if time.Since(msg.CreatedAt) > 2*time.Minute {
		h.sendError(c, "超过 2 分钟，无法撤回")
		return
	}
	// 幂等：已经撤回过的直接返回，既不重复扣未读、也不重复广播
	changed, err := h.deps.Messages.MarkRecalled(msg.ID)
	if err != nil {
		slog.Error("ws recall", "err", err.Error())
		return
	}
	if !changed {
		return
	}
	if err := h.deps.Messages.RecallUnreadAdjust(msg.ConversationID, c.UserID, uint64(msg.ID)); err != nil {
		slog.Error("ws recall unread adjust", "err", err.Error())
	}
	slog.Info("message recalled", "msg", msg.ID, "conv", msg.ConversationID, "by", c.UserID)
	ids, err := h.deps.Convs.MemberUserIDs(msg.ConversationID)
	if err != nil {
		return
	}
	h.ToUsers(ids, ginH{"type": "recall", "messageId": msg.ID, "convId": msg.ConversationID})
}

// checkConnState 连接级封禁 / token_version 复核（handleChat 落库前调用）。
//
// 修复缺陷：IsBanned / TokenVersion 此前只在 serve.go 握手段校验，连接建立后
// admin 封禁或重置密码（tv+1）对既有连接无效 —— 忽略 kicked 帧即可继续发消息、
// 写库、fan-out。本函数在每次 chat 落库前复核一次：
//   - 用户已封禁 → error 帧 + 断开；
//   - 库中 tv 与握手缓存 tv 不一致 → error 帧 + 断开（密码被重置 / 强制重登）。
//
// deps.Users 允许为 nil（NewHub(nil) 单测 / 未注入），此时跳过复核以保持兼容。
func (h *Hub) checkConnState(c *Client, convID uint) bool {
	if h.deps == nil || h.deps.Users == nil {
		return true
	}
	u, err := h.deps.Users.ByID(c.UserID)
	if err != nil {
		slog.Error("ws conn state lookup", "err", err.Error(), "uid", c.UserID)
		h.sendError(c, "服务器开小差了，请稍后再试")
		return false
	}
	if u == nil {
		// 账号已被删除：旧连接必须立即失效
		h.denyConn(c, convID, "登录已失效，请重新登录")
		return false
	}
	if u.IsBanned() {
		slog.Warn("ws chat rejected: banned", "uid", c.UserID, "conv", convID)
		h.denyConn(c, convID, "账号已被封禁")
		return false
	}
	if u.TokenVersion != c.tv {
		slog.Warn("ws chat rejected: stale token version",
			"uid", c.UserID, "conn_tv", c.tv, "db_tv", u.TokenVersion)
		h.denyConn(c, convID, "登录已失效，请重新登录")
		return false
	}
	return true
}

// denyConn 回 error 帧并强制断开该连接（连接级复核失败时使用）。
// requestKick 幂等；这里不 close(c.send)，所有权仍属 Unregister。
func (h *Hub) denyConn(c *Client, convID uint, reason string) {
	h.sendError(c, reason)
	// 先冲刷队列把 error 帧送达，再关闭连接（与 KickUser 语义一致）。
	c.requestKick(true)
}

// requireMember 上行帧的会话成员校验。
func (h *Hub) requireMember(c *Client, convID uint) bool {
	if convID == 0 || h.deps == nil {
		return false
	}
	m, err := h.deps.Convs.MemberOf(convID, c.UserID)
	if err != nil || m == nil {
		// 非成员上行帧可能来自被踢后的残留客户端，也可能是越权探测：记 info 便于排查
		slog.Info("ws frame rejected: not a member", "uid", c.UserID, "conv", convID, "err", errStr(err))
		h.sendError(c, "无权访问该会话")
		return false
	}
	return true
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (h *Hub) sendError(c *Client, reason string) {
	c.trySend(mustJSON(ginH{"type": "error", "reason": reason}))
}

func messageFrame(m *model.Message, clientMsgID string) ginH {
	frame := ginH{
		"type":      "message",
		"id":        m.ID,
		"convId":    m.ConversationID,
		"from":      ginH{"id": m.SenderID},
		"content":   m.Content,
		"msgType":   m.MsgType,
		"createdAt": m.CreatedAt,
	}
	if clientMsgID != "" {
		frame["clientMsgId"] = clientMsgID
	}
	return frame
}

func isSafeMsgType(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		ok := b == '-' || b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z')
		if !ok {
			return false
		}
	}
	return utf8.RuneCountInString(s) > 0
}

func msgIDPtr(id uint64) *uint64 { return &id }

func withoutID(ids []uint, exclude uint) []uint {
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id != exclude {
			out = append(out, id)
		}
	}
	return out
}

func ukey(uid uint) string {
	// 稳定字符串 key（避免 rune 转换歧义）
	const digits = "0123456789"
	if uid == 0 {
		return "u0"
	}
	buf := [20]byte{}
	i := len(buf)
	for uid > 0 {
		i--
		buf[i] = digits[uid%10]
		uid /= 10
	}
	return "u" + string(buf[i:])
}
