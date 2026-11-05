package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// ModerationHandler 治理：拉黑、举报、管理员删除违规消息。
type ModerationHandler struct {
	users *store.UserStore
	convs *store.ConversationStore
	msgs  *store.MessageStore
	mod   *store.ModerationStore
}

func NewModerationHandler(
	users *store.UserStore,
	convs *store.ConversationStore,
	msgs *store.MessageStore,
	mod *store.ModerationStore,
) *ModerationHandler {
	return &ModerationHandler{users: users, convs: convs, msgs: msgs, mod: mod}
}

// BlockStatus 查询我是否拉黑了目标用户。
func (h *ModerationHandler) BlockStatus(c *gin.Context) {
	targetID, ok := parseUserID(c)
	if !ok {
		return
	}
	blocked, err := h.mod.IsBlocked(middleware.UserFrom(c).ID, targetID)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"blocked": blocked})
}

// Block 拉黑用户（幂等）。
func (h *ModerationHandler) Block(c *gin.Context) {
	targetID, ok := parseUserID(c)
	if !ok {
		return
	}
	me := middleware.UserFrom(c)
	if targetID == me.ID {
		fail(c, http.StatusUnprocessableEntity, "不能拉黑自己")
		return
	}
	target, err := h.users.ByID(targetID)
	if err != nil {
		internal(c, err)
		return
	}
	if target == nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if err := h.mod.Block(me.ID, targetID); err != nil {
		internal(c, err)
		return
	}
	slog.Info("user blocked", "blocker", me.ID, "blocked", targetID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Unblock 取消拉黑（幂等）。
func (h *ModerationHandler) Unblock(c *gin.Context) {
	targetID, ok := parseUserID(c)
	if !ok {
		return
	}
	me := middleware.UserFrom(c)
	if err := h.mod.Unblock(me.ID, targetID); err != nil {
		internal(c, err)
		return
	}
	slog.Info("user unblocked", "blocker", me.ID, "blocked", targetID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Report 举报消息。举报者必须是消息所在会话的成员，且消息未被撤回。
func (h *ModerationHandler) Report(c *gin.Context) {
	msgID, ok := parseMessageID(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !bind(c, &req) {
		return
	}
	msg, err := h.msgs.ByID(msgID)
	if err != nil {
		internal(c, err)
		return
	}
	if msg == nil || msg.AdminDeletedAt != nil {
		fail(c, http.StatusNotFound, "消息不存在")
		return
	}
	m, err := h.convs.MemberOf(msg.ConversationID, middleware.UserFrom(c).ID)
	if err != nil {
		internal(c, err)
		return
	}
	if m == nil {
		fail(c, http.StatusForbidden, "无权访问该会话")
		return
	}
	if msg.IsRecalled {
		fail(c, http.StatusUnprocessableEntity, "已撤回的消息无需举报")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if n := len([]rune(reason)); n < 1 || n > 200 {
		fail(c, http.StatusBadRequest, "举报理由需为 1-200 个字符")
		return
	}
	created, err := h.mod.Report(middleware.UserFrom(c).ID, msg.ID, reason)
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("message reported", "msg", msg.ID, "conv", msg.ConversationID,
		"by", middleware.UserFrom(c).ID, "duplicate", !created)
	c.JSON(http.StatusOK, gin.H{"ok": true, "duplicate": !created})
}

// AdminDeleteMessage 管理员删除违规消息：站点管理员，或该群群主 / 管理员。
// 消息本体保留（审计用），仅通过查询过滤让所有成员不可见。
func (h *ModerationHandler) AdminDeleteMessage(c *gin.Context) {
	msgID, ok := parseMessageID(c)
	if !ok {
		return
	}
	msg, err := h.msgs.ByID(msgID)
	if err != nil {
		internal(c, err)
		return
	}
	if msg == nil {
		fail(c, http.StatusNotFound, "消息不存在")
		return
	}
	me := middleware.UserFrom(c)
	allowed := me.Role == "admin"
	if !allowed {
		conv, err := h.convs.ByID(msg.ConversationID)
		if err != nil {
			internal(c, err)
			return
		}
		if conv != nil && conv.Type == 2 {
			if m, err := h.convs.MemberOf(msg.ConversationID, me.ID); err != nil {
				internal(c, err)
				return
			} else {
				allowed = m != nil && (m.Role == "owner" || m.Role == "admin")
			}
		}
	}
	if !allowed {
		fail(c, http.StatusForbidden, "无权删除该消息")
		return
	}

	changed, err := h.mod.AdminDeleteMessage(msg.ID, me.ID)
	if err != nil {
		internal(c, err)
		return
	}
	if !changed {
		// 已经删过：幂等返回，避免前端重复点击报错
		c.JSON(http.StatusOK, gin.H{"ok": true, "duplicate": true})
		return
	}
	slog.Info("message admin-deleted", "msg", msg.ID, "conv", msg.ConversationID, "by", me.ID)

	ids, err := h.convs.MemberUserIDs(msg.ConversationID)
	if err != nil {
		internal(c, err)
		return
	}
	// 广播给所有成员（含发送者）：各端都要把这条从聊天记录里移除
	notifyFor(c).ToUsers(ids, gin.H{
		"type": "message_admin_deleted", "messageId": msg.ID, "convId": msg.ConversationID,
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
