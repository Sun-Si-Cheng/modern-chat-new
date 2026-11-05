package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// EditWindow 消息编辑时限（与前端 canEdit 的判断保持一致）。
const EditWindow = 5 * time.Minute

// 上行帧（§7）：宽松解析，按 type 路由。
type clientFrame struct {
	Type        string `json:"type"`
	ConvID      uint   `json:"convId"`
	ClientMsgID string `json:"clientMsgId"`
	Content     string `json:"content"`
	MsgType     string `json:"msgType"`
	UpToID      uint64 `json:"upToId"`
	MessageID   uint64 `json:"messageId"`
	// ReplyToID 引用消息 id；Mentions 被 @ 的用户 id（均为可选）
	ReplyToID *uint64 `json:"replyToId"`
	Mentions  []uint  `json:"mentions"`
	// Forwarded 标记这是一条转发消息（只影响展示，不改变投递逻辑）
	Forwarded bool `json:"forwarded"`
	// Emoji / Action 用于表情回应：action=set|remove
	Emoji  string `json:"emoji"`
	Action string `json:"action"`
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
	case "edit":
		h.handleEdit(c, &f)
	case "reaction":
		h.handleReaction(c, &f)
	case "delivered":
		h.handleDelivered(c, &f)
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
	// 禁言：到时间自动失效；命中时直接拒绝发送
	if h.memberMuted(c, f.ConvID) {
		h.sendError(c, "你已被禁言，暂时不能发送消息")
		return
	}
	// 全员禁言：群主与管理员不受影响
	conv, err := h.deps.Convs.ByID(f.ConvID)
	if err != nil {
		slog.Error("ws conversation lookup", "err", err.Error(), "conv", f.ConvID)
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}
	if conv != nil && conv.MuteAll {
		m, _ := h.deps.Convs.MemberOf(f.ConvID, c.UserID)
		if m == nil || (m.Role != "owner" && m.Role != "admin") {
			h.sendError(c, "群已开启全员禁言，只有群主和管理员可以发言")
			return
		}
	}
	// 单聊拉黑：任一方向拉黑后双方都不能再发送（拉黑即互不打扰）
	if conv != nil && conv.Type == 1 && h.deps.Moderation != nil {
		peerID, pErr := h.deps.Convs.OtherMemberID(f.ConvID, c.UserID)
		if pErr != nil {
			slog.Error("ws single peer lookup", "err", pErr.Error(), "conv", f.ConvID)
			h.sendError(c, "服务器开小差了，请稍后再试")
			return
		}
		if peerID > 0 {
			if blocked, bErr := h.deps.Moderation.BlockedEither(c.UserID, peerID); bErr != nil {
				slog.Error("ws block lookup", "err", bErr.Error())
				h.sendError(c, "服务器开小差了，请稍后再试")
				return
			} else if blocked {
				h.sendError(c, "你们已互相拉黑，无法发送消息")
				return
			}
		}
	}
	// 消息长度上限与敏感词可在后台调整，默认 1000 / 无敏感词
	ops := store.DefaultOps()
	if h.deps.Settings != nil {
		ops = h.deps.Settings.Ops()
	}
	maxLen := ops.MaxMessageLength
	content := []rune(f.Content)
	if len(content) < 1 || len(content) > maxLen {
		h.sendError(c, fmt.Sprintf("消息需为 1-%d 个字符", maxLen))
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
	if msgType == "text" && hitBlockedWord(ops.BlockedWords, string(content)) {
		h.sendError(c, "消息包含敏感词，请修改后重发")
		return
	}
	// 媒体消息（图片/语音/表情/视频）的 content 必须是本站上传目录下的地址。
	// 不校验的话用户可以塞 https://evil.com/x.mp4，接收方客户端会去加载外部资源
	// （泄露 IP、可被打点统计），而消息看起来完全正常。
	if msgType == "image" || msgType == "audio" || msgType == "sticker" || msgType == "video" {
		// 由存储实现自己判断 URL 归属（本地是 /uploads/，S3 是配置的对外域名），
		// 这样换存储不用改校验逻辑。测试里未注入 Storage 时退回本地前缀判断。
		var ok bool
		if h.deps.Storage != nil {
			ok = h.deps.Storage.Owns(string(content))
		} else {
			ok = strings.HasPrefix(string(content), "/uploads/") && !strings.Contains(string(content), "..")
		}
		if !ok {
			h.sendError(c, "媒体消息的内容必须是本站上传地址")
			return
		}
	}

	// 幂等：同 client_msg_id 直接回放原消息
	if existing, err := h.deps.Messages.ByClientMsg(f.ConvID, f.ClientMsgID); err != nil {
		slog.Error("ws idempotent lookup", "err", err.Error())
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	} else if existing != nil {
		// 回放也要带上引用摘要：断线重发时前端会用这一帧替换本地乐观消息，
		// 缺了 replyTo 引用块就会凭空消失
		one := []model.Message{*existing}
		if aErr := h.deps.Messages.AttachReply(one); aErr == nil {
			existing.ReplyTo = one[0].ReplyTo
		}
		c.trySend(mustJSON(messageFrame(existing, f.ClientMsgID)))
		return
	}

	// 成员列表：@ 校验与收件箱冗余都要用，只取一次
	memberIDs, err := h.deps.Convs.MemberUserIDs(f.ConvID)
	if err != nil {
		slog.Error("ws members", "err", err.Error())
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}

	// 引用必须指向同一条会话里的消息：
	// 不校验的话可以引用别的会话的消息 id，把那个会话的内容透出来。
	var replyTo *uint64
	if f.ReplyToID != nil && *f.ReplyToID > 0 {
		target, tErr := h.deps.Messages.ByID(uint(*f.ReplyToID))
		if tErr != nil || target == nil || target.ConversationID != f.ConvID {
			h.sendError(c, "引用的消息不存在")
			return
		}
		replyTo = f.ReplyToID
	}

	// @ 只保留「确实在群里」的 id，并去掉自己
	mentions := resolveMentions(memberIDs, c.UserID, f.Mentions)

	msg := &model.Message{
		ConversationID: f.ConvID,
		SenderID:       c.UserID,
		ClientMsgID:    f.ClientMsgID,
		MsgType:        msgType,
		Content:        string(content),
		ReplyToID:      replyTo,
		Mentions:       mentions,
		Forwarded:      f.Forwarded,
		CreatedAt:      time.Now(),
	}
	if err := h.deps.Messages.Create(msg); err != nil {
		slog.Error("ws create message", "err", err.Error(), "uid", c.UserID, "conv", f.ConvID)
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}
	// 广播帧要带引用摘要：单条装配一次（复用批量实现，避免再写一套查询）
	one := []model.Message{*msg}
	if aErr := h.deps.Messages.AttachReply(one); aErr == nil {
		msg.ReplyTo = one[0].ReplyTo
	}

	// 消息正文不入日志（隐私），只记元数据
	slog.Debug("message stored",
		"msg", msg.ID, "conv", msg.ConversationID, "from", msg.SenderID,
		"type", msg.MsgType, "chars", len([]rune(msg.Content)))

	// 收件箱冗余：全员 last=新消息；新成员行在此首次创建（memberIDs 已在上方取过）
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
	// 被 @ 的人即使把会话设成免打扰，也要看到「有人@我」
	if len(msg.Mentions) > 0 {
		if err := h.deps.Messages.MarkMention(f.ConvID, msg.Mentions); err != nil {
			slog.Error("ws mark mention", "err", err.Error())
		}
	}

	// fan-out：message 帧；非发送者额外 conv_change
	mf := messageFrame(msg, f.ClientMsgID)
	others := withoutID(memberIDs, c.UserID)
	h.ToUsers(memberIDs, mf)
	h.ToUsers(others, ginH{"type": "conv_change", "convId": f.ConvID})
	// 离线用户补一条 Web Push（在线用户已经收到桌面通知，不重复打扰）
	h.notifyPush(others, msg)
}

// notifyPush 给离线成员推送浏览器通知。失败只记日志，不影响消息投递。
func (h *Hub) notifyPush(userIDs []uint, msg *model.Message) {
	if h.deps == nil || h.deps.Push == nil || !h.deps.Push.Enabled() || h.deps.PushSubs == nil {
		return
	}
	body := pushPreview(msg)
	payload := ginH{"title": "ModernChat", "body": body, "convId": msg.ConversationID, "url": "/chat"}
	for _, uid := range userIDs {
		if uid == 0 || h.IsOnline(uid) {
			continue
		}
		go func(uid uint) {
			subs, err := h.deps.PushSubs.ForUser(uid)
			if err != nil {
				slog.Warn("push subscriptions lookup failed", "uid", uid, "err", err.Error())
				return
			}
			for i := range subs {
				if err := h.deps.Push.Send(&subs[i], payload); err != nil {
					if errors.Is(err, service.ErrPushGone) {
						_ = h.deps.PushSubs.Remove(subs[i].Endpoint)
					} else {
						slog.Warn("push send failed", "uid", uid, "err", err.Error())
					}
				}
			}
		}(uid)
	}
}

func pushPreview(m *model.Message) string {
	switch m.MsgType {
	case "image":
		return "[图片]"
	case "audio":
		return "[语音]"
	case "sticker":
		return "[表情]"
	case "video":
		return "[视频]"
	}
	s := m.Content
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60]) + "…"
	}
	return s
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
	// 广播给包括自己在内的所有端：同账号其他设备也要把未读清掉
	h.ToUsers(ids, ginH{
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
	// 撤回时限可配（0 表示禁止撤回）
	recallMinutes := store.DefaultOps().RecallMinutes
	if h.deps.Settings != nil {
		recallMinutes = h.deps.Settings.Ops().RecallMinutes
	}
	if recallMinutes <= 0 {
		h.sendError(c, "本站已关闭消息撤回")
		return
	}
	if time.Since(msg.CreatedAt) > time.Duration(recallMinutes)*time.Minute {
		h.sendError(c, fmt.Sprintf("超过 %d 分钟，无法撤回", recallMinutes))
		return
	}
	// 幂等：已经撤回过的直接返回，既不重复扣未读、也不重复广播
	changed, recalledAt, err := h.deps.Messages.MarkRecalled(msg.ID)
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
	// 广播带上服务端撤回时间：所有端用同一个时间点计算「提示显示满 3 分钟」
	h.ToUsers(ids, ginH{
		"type":       "recall",
		"messageId":  msg.ID,
		"convId":     msg.ConversationID,
		"recalledAt": recalledAt.UTC().Format(time.RFC3339Nano),
	})
}

// handleDelivered 单聊送达回执：接收方客户端确认已收到，转发给发送者用于展示「已送达」。
func (h *Hub) handleDelivered(c *Client, f *clientFrame) {
	if f.MessageID == 0 || h.deps == nil || h.deps.Messages == nil {
		return
	}
	msg, err := h.deps.Messages.ByID(uint(f.MessageID))
	if err != nil || msg == nil || msg.SenderID == c.UserID {
		return
	}
	if !h.requireMember(c, msg.ConversationID) {
		return
	}
	conv, err := h.deps.Convs.ByID(msg.ConversationID)
	if err != nil || conv == nil || conv.Type != 1 {
		return
	}
	h.ToUsers([]uint{msg.SenderID}, ginH{
		"type": "message_delivered", "messageId": msg.ID, "convId": msg.ConversationID,
		"userId": c.UserID,
	})
}

// handleEdit 编辑自己的文本消息：5 分钟内、未撤回、仅文本。
func (h *Hub) handleEdit(c *Client, f *clientFrame) {
	if f.MessageID == 0 || h.deps == nil || h.deps.Messages == nil {
		return
	}
	msg, err := h.deps.Messages.ByID(uint(f.MessageID))
	if err != nil {
		slog.Error("ws edit lookup", "err", err.Error(), "uid", c.UserID)
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}
	if msg == nil || msg.SenderID != c.UserID {
		h.sendError(c, "只能编辑自己的消息")
		return
	}
	if msg.IsRecalled {
		h.sendError(c, "已撤回的消息不能编辑")
		return
	}
	if msg.MsgType != "text" {
		h.sendError(c, "只有文本消息可以编辑")
		return
	}
	if time.Since(msg.CreatedAt) > EditWindow {
		h.sendError(c, "超过 5 分钟，无法编辑")
		return
	}
	if !h.requireMember(c, msg.ConversationID) {
		return
	}
	if h.memberMuted(c, msg.ConversationID) {
		h.sendError(c, "你已被禁言，暂时不能编辑消息")
		return
	}
	ops := store.DefaultOps()
	if h.deps.Settings != nil {
		ops = h.deps.Settings.Ops()
	}
	maxLen := ops.MaxMessageLength
	content := []rune(f.Content)
	if len(content) < 1 || len(content) > maxLen {
		h.sendError(c, fmt.Sprintf("消息需为 1-%d 个字符", maxLen))
		return
	}
	if hitBlockedWord(ops.BlockedWords, string(content)) {
		h.sendError(c, "消息包含敏感词，请修改后重发")
		return
	}
	now, err := h.deps.Messages.UpdateContent(msg.ID, string(content))
	if err != nil {
		slog.Error("ws edit update", "err", err.Error(), "msg", msg.ID)
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}
	ids, err := h.deps.Convs.MemberUserIDs(msg.ConversationID)
	if err != nil {
		return
	}
	h.ToUsers(ids, ginH{
		"type":     "message_update",
		"id":       msg.ID,
		"convId":   msg.ConversationID,
		"content":  string(content),
		"editedAt": now.UTC().Format(time.RFC3339Nano),
	})
	slog.Debug("message edited", "msg", msg.ID, "conv", msg.ConversationID, "uid", c.UserID)
}

// handleReaction 表情回应：每人每条消息最多一个表情；action=set|remove。
func (h *Hub) handleReaction(c *Client, f *clientFrame) {
	if f.MessageID == 0 || h.deps == nil || h.deps.Messages == nil {
		return
	}
	msg, err := h.deps.Messages.ByID(uint(f.MessageID))
	if err != nil || msg == nil || msg.IsRecalled {
		return
	}
	if !h.requireMember(c, msg.ConversationID) {
		return
	}
	emoji := strings.TrimSpace(f.Emoji)
	if n := len([]rune(emoji)); n < 1 || n > 4 {
		h.sendError(c, "表情不合法")
		return
	}
	ids, err := h.deps.Convs.MemberUserIDs(msg.ConversationID)
	if err != nil {
		return
	}
	if f.Action == "remove" {
		if err := h.deps.Messages.RemoveReaction(msg.ID, c.UserID); err != nil {
			slog.Error("ws reaction remove", "err", err.Error())
			return
		}
		h.ToUsers(ids, ginH{
			"type": "reaction", "messageId": msg.ID, "convId": msg.ConversationID,
			"emoji": emoji, "userId": c.UserID, "action": "remove",
		})
		return
	}
	before, err := h.deps.Messages.SetReaction(msg.ID, c.UserID, emoji)
	if err != nil {
		slog.Error("ws reaction set", "err", err.Error())
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}
	// 已经就是这个表情：幂等，不重复广播
	if before == emoji {
		return
	}
	h.ToUsers(ids, ginH{
		"type": "reaction", "messageId": msg.ID, "convId": msg.ConversationID,
		"emoji": emoji, "before": before, "userId": c.UserID, "action": "set",
	})
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

// memberMuted 调用者在该会话是否处于禁言期。
func (h *Hub) memberMuted(c *Client, convID uint) bool {
	if h.deps == nil || h.deps.Convs == nil {
		return false
	}
	m, err := h.deps.Convs.MemberOf(convID, c.UserID)
	if err != nil || m == nil {
		return false
	}
	return m.MuteUntil != nil && m.MuteUntil.After(time.Now())
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

// resolveMentions 过滤 @ 目标：只留会话成员、去重、排除发送者，上限 20 个。
//
// 不校验的话客户端可以 @ 任意 user id，把「有人@我」的提示推给无关的人。
func resolveMentions(memberIDs []uint, senderID uint, want []uint) []uint {
	if len(want) == 0 {
		return nil
	}
	allowed := make(map[uint]struct{}, len(memberIDs))
	for _, id := range memberIDs {
		allowed[id] = struct{}{}
	}
	out := make([]uint, 0, len(want))
	seen := make(map[uint]struct{}, len(want))
	for _, id := range want {
		if id == 0 || id == senderID {
			continue
		}
		if _, ok := allowed[id]; !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
		if len(out) >= 20 {
			break
		}
	}
	return out
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
	// 引用摘要随帧下发：接收方不用再回查，直接能渲染引用块
	if m.ReplyTo != nil {
		frame["replyTo"] = m.ReplyTo
	}
	if m.ReplyToID != nil {
		frame["replyToId"] = *m.ReplyToID
	}
	if len(m.Mentions) > 0 {
		frame["mentions"] = m.Mentions
	}
	if m.Forwarded {
		frame["forwarded"] = true
	}
	return frame
}

// hitBlockedWord 文本是否命中后台配置的敏感词（子串匹配，大小写敏感）。
func hitBlockedWord(words []string, content string) bool {
	for _, w := range words {
		if w != "" && strings.Contains(content, w) {
			return true
		}
	}
	return false
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
