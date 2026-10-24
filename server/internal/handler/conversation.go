package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// maxGroupMembers 群成员上限（§3.2）。
const maxGroupMembers int64 = 200

// ConversationHandler 会话与消息 REST。
type ConversationHandler struct {
	users   *store.UserStore
	friends *store.FriendStore
	convs   *store.ConversationStore
	msgs    *store.MessageStore
}

func NewConversationHandler(users *store.UserStore, friends *store.FriendStore, convs *store.ConversationStore, msgs *store.MessageStore) *ConversationHandler {
	return &ConversationHandler{users: users, friends: friends, convs: convs, msgs: msgs}
}

// List 会话列表。
func (h *ConversationHandler) List(c *gin.Context) {
	rows, err := h.convs.ListFor(middleware.UserFrom(c).ID)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// SearchGroups 搜群。
func (h *ConversationHandler) SearchGroups(c *gin.Context) {
	kw := c.Query("kw")
	if len([]rune(kw)) == 0 || len([]rune(kw)) > 50 {
		fail(c, http.StatusBadRequest, "关键词需为 1-50 个字符")
		return
	}
	rows, err := h.convs.SearchGroups(kw, 20)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// CreateSingle 发起（或复用）单聊：双方须为好友。
func (h *ConversationHandler) CreateSingle(c *gin.Context) {
	var req struct {
		ToUserID uint `json:"toUserId"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	if req.ToUserID == me.ID {
		fail(c, http.StatusUnprocessableEntity, "不能和自己发起单聊")
		return
	}
	target, err := h.users.ByID(req.ToUserID)
	if err != nil {
		internal(c, err)
		return
	}
	if target == nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	friends, err := h.friends.PendingBetween(me.ID, target.ID, 1)
	if err != nil {
		internal(c, err)
		return
	}
	if len(friends) == 0 {
		fail(c, http.StatusUnprocessableEntity, "只能与好友发起单聊")
		return
	}

	if existing, err := h.convs.SingleExists(me.ID, target.ID); err != nil {
		internal(c, err)
		return
	} else if existing != nil {
		c.JSON(http.StatusOK, h.buildSingleView(existing, target, me.ID))
		return
	}

	conv := &model.Conversation{Type: 1}
	if err := h.convs.Create(conv); err != nil {
		internal(c, err)
		return
	}
	members := []model.ConversationMember{
		{ConversationID: conv.ID, UserID: me.ID, Role: "member"},
		{ConversationID: conv.ID, UserID: target.ID, Role: "member"},
	}
	if err := h.convs.AddMembers(members); err != nil {
		internal(c, err)
		return
	}
	if err := h.initInboxes(conv.ID, me.ID, target.ID); err != nil {
		internal(c, err)
		return
	}
	// 通知对方刷新列表
	notifyFor(c).ToUsers([]uint{target.ID}, gin.H{"type": "conv_change", "convId": conv.ID})
	slog.Info("conversation created", "type", "single", "conv", conv.ID, "a", me.ID, "b", target.ID)
	c.JSON(http.StatusOK, h.buildSingleView(conv, target, me.ID))
}

// CreateGroup 建群（创建者为群主）。
func (h *ConversationHandler) CreateGroup(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if !bind(c, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if n := len([]rune(name)); n < 1 || n > 30 {
		fail(c, http.StatusBadRequest, "群名称需为 1-30 个字符")
		return
	}
	me := middleware.UserFrom(c)

	conv := &model.Conversation{Type: 2, Name: &name, OwnerID: &me.ID}
	if err := h.convs.Create(conv); err != nil {
		internal(c, err)
		return
	}
	if err := h.convs.AddMembers([]model.ConversationMember{
		{ConversationID: conv.ID, UserID: me.ID, Role: "owner"},
	}); err != nil {
		internal(c, err)
		return
	}
	if err := h.initInboxes(conv.ID, me.ID); err != nil {
		internal(c, err)
		return
	}
	slog.Info("conversation created", "type", "group", "conv", conv.ID, "owner", me.ID, "name", name)
	c.JSON(http.StatusOK, model.ConversationView{
		ID: conv.ID, Type: conv.Type, Name: conv.Name, OwnerID: conv.OwnerID, MemberCount: 1,
	})
}

// Messages 历史分页（游标 beforeId，降序）。
func (h *ConversationHandler) Messages(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	if !h.requireMember(c, convID) {
		return
	}
	beforeID := parseUint64(c.Query("beforeId"))
	limit := int(parseUint64Default(c.Query("limit"), 30))
	if limit < 1 || limit > 50 {
		limit = 30
	}
	rows, hasMore, err := h.msgs.PageBefore(convID, beforeID, limit)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": rows, "hasMore": hasMore})
}

// Members 成员列表。
func (h *ConversationHandler) Members(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	if !h.requireMember(c, convID) {
		return
	}
	rows, err := h.convs.MembersView(convID)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// Join 加群。
func (h *ConversationHandler) Join(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	me := middleware.UserFrom(c)
	conv, err := h.convs.ByID(convID)
	if err != nil {
		internal(c, err)
		return
	}
	if conv == nil {
		fail(c, http.StatusNotFound, "会话不存在")
		return
	}
	if conv.Type != 2 {
		fail(c, http.StatusUnprocessableEntity, "单聊无需加入")
		return
	}
	if m, err := h.convs.MemberOf(convID, me.ID); err != nil {
		internal(c, err)
		return
	} else if m != nil {
		fail(c, http.StatusConflict, "你已在群中")
		return
	}
	// 原子入群：容量判断与插入在同一事务内（SELECT ... FOR UPDATE 锁会话行），
	// 修复并发入群可突破 200 人上限的缺陷。
	if err := h.convs.JoinGroup(convID, me.ID, maxGroupMembers); err != nil {
		if errors.Is(err, store.ErrGroupFull) {
			fail(c, http.StatusUnprocessableEntity, "群成员已满（200 人上限）")
			return
		}
		internal(c, err)
		return
	}
	if err := h.initInboxes(convID, me.ID); err != nil {
		internal(c, err)
		return
	}
	ids, err := h.convs.MemberUserIDs(convID)
	if err != nil {
		internal(c, err)
		return
	}
	notifyFor(c).ToUsers(ids, gin.H{"type": "conv_change", "convId": convID})
	slog.Info("conversation joined", "conv", convID, "uid", me.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Leave 退群（群主须先转让）。
func (h *ConversationHandler) Leave(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	me := middleware.UserFrom(c)
	m, err := h.convs.MemberOf(convID, me.ID)
	if err != nil {
		internal(c, err)
		return
	}
	if m == nil {
		fail(c, http.StatusNotFound, "会话不存在")
		return
	}
	if m.Role == "owner" {
		fail(c, http.StatusUnprocessableEntity, "群主不能退群，请先转让")
		return
	}
	if err := h.leaveCleanup(convID, me.ID); err != nil {
		internal(c, err)
		return
	}
	ids, err := h.convs.MemberUserIDs(convID)
	if err != nil {
		internal(c, err)
		return
	}
	notifyFor(c).ToUsers(ids, gin.H{"type": "conv_change", "convId": convID})
	slog.Info("conversation left", "conv", convID, "uid", me.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Read 上报已读。
func (h *ConversationHandler) Read(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	var req struct {
		UpToID uint64 `json:"upToId"`
	}
	if !bind(c, &req) || req.UpToID == 0 {
		fail(c, http.StatusBadRequest, "upToId 不合法")
		return
	}
	me := middleware.UserFrom(c)
	if !h.requireMember(c, convID) {
		return
	}
	// 修复缺陷：原实现不校验 upToId 归属，可把别的会话的消息 id 当作本会话已读游标，
	// 造成 last_read_message_id 被写成越界值（未读计数错乱）。要求该消息存在且属于本会话。
	msg, err := h.msgs.ByID(uint(req.UpToID))
	if err != nil {
		internal(c, err)
		return
	}
	if msg == nil || msg.ConversationID != convID {
		fail(c, http.StatusBadRequest, "upToId 不合法")
		return
	}
	affected, err := h.msgs.MarkRead(me.ID, convID, req.UpToID)
	if err != nil {
		internal(c, err)
		return
	}
	if affected > 0 {
		if ids, err := h.convs.MemberUserIDs(convID); err == nil {
			others := without(ids, me.ID)
			notifyFor(c).ToUsers(others, gin.H{
				"type": "read", "convId": convID, "userId": me.ID, "upToId": req.UpToID,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Rename 群主改群名。
func (h *ConversationHandler) Rename(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !bind(c, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if n := len([]rune(name)); n < 1 || n > 30 {
		fail(c, http.StatusBadRequest, "群名称需为 1-30 个字符")
		return
	}
	me := middleware.UserFrom(c)
	if !h.requireOwner(c, convID, me.ID) {
		return
	}
	if err := h.convs.Rename(convID, name); err != nil {
		internal(c, err)
		return
	}
	slog.Info("conversation renamed", "conv", convID, "by", me.ID, "name", name)
	ids, err := h.convs.MemberUserIDs(convID)
	if err != nil {
		internal(c, err)
		return
	}
	notifyFor(c).ToUsers(ids, gin.H{"type": "conv_change", "convId": convID})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Kick 群主移出成员。
func (h *ConversationHandler) Kick(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	var req struct {
		UserID uint `json:"userId"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	if !h.requireOwner(c, convID, me.ID) {
		return
	}
	if req.UserID == me.ID {
		fail(c, http.StatusUnprocessableEntity, "不能移除自己，请使用转让或退群")
		return
	}
	if m, err := h.convs.MemberOf(convID, req.UserID); err != nil {
		internal(c, err)
		return
	} else if m == nil {
		fail(c, http.StatusUnprocessableEntity, "该用户不在群中")
		return
	} else if m.Role == "owner" {
		// 修复缺陷：规格 §3.2 要求群主不可被移除；原实现只挡了「移除自己」，
		// 若数据里出现第二个 owner 角色行（转让竞态等），群主仍可能被踢掉。
		fail(c, http.StatusUnprocessableEntity, "不能移除群主")
		return
	}
	if err := h.leaveCleanup(convID, req.UserID); err != nil {
		internal(c, err)
		return
	}
	ids, err := h.convs.MemberUserIDs(convID)
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("member kicked", "conv", convID, "target", req.UserID, "by", me.ID)
	notifyFor(c).ToUsers(append(ids, req.UserID), gin.H{"type": "conv_change", "convId": convID})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Transfer 转让群主。
func (h *ConversationHandler) Transfer(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	var req struct {
		UserID uint `json:"userId"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	if !h.requireOwner(c, convID, me.ID) {
		return
	}
	if req.UserID == me.ID {
		fail(c, http.StatusUnprocessableEntity, "已是群主")
		return
	}
	if m, err := h.convs.MemberOf(convID, req.UserID); err != nil {
		internal(c, err)
		return
	} else if m == nil {
		fail(c, http.StatusUnprocessableEntity, "该用户不在群中")
		return
	}
	if err := h.convs.SetOwner(convID, req.UserID, me.ID); err != nil {
		internal(c, err)
		return
	}
	slog.Info("conversation owner transferred", "conv", convID, "from", me.ID, "to", req.UserID)
	ids, err := h.convs.MemberUserIDs(convID)
	if err != nil {
		internal(c, err)
		return
	}
	notifyFor(c).ToUsers(ids, gin.H{"type": "conv_change", "convId": convID})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- 内部辅助 ----

// requireMember 会话存在且调用者是成员；否则 404 / 403。
func (h *ConversationHandler) requireMember(c *gin.Context, convID uint) bool {
	conv, err := h.convs.ByID(convID)
	if err != nil {
		internal(c, err)
		return false
	}
	if conv == nil {
		fail(c, http.StatusNotFound, "会话不存在")
		return false
	}
	me := middleware.UserFrom(c)
	m, err := h.convs.MemberOf(convID, me.ID)
	if err != nil {
		internal(c, err)
		return false
	}
	if m == nil {
		fail(c, http.StatusForbidden, "无权访问该会话")
		return false
	}
	return true
}

// requireOwner 会话存在且调用者是群主。
func (h *ConversationHandler) requireOwner(c *gin.Context, convID, userID uint) bool {
	if !h.requireMember(c, convID) {
		return false
	}
	m, err := h.convs.MemberOf(convID, userID)
	if err != nil {
		internal(c, err)
		return false
	}
	if m == nil || m.Role != "owner" {
		fail(c, http.StatusForbidden, "无权执行此操作")
		return false
	}
	return true
}

// initInboxes 初始化收件箱行（无历史未读）。
func (h *ConversationHandler) initInboxes(convID uint, userIDs ...uint) error {
	rows := make([]model.UserConversation, 0, len(userIDs))
	for _, uid := range userIDs {
		rows = append(rows, model.UserConversation{UserID: uid, ConversationID: convID})
	}
	return h.msgs.UpsertInbox(rows)
}

// leaveCleanup 移除成员与其收件箱行。
func (h *ConversationHandler) leaveCleanup(convID, userID uint) error {
	if err := h.convs.RemoveMember(convID, userID); err != nil {
		return err
	}
	return h.msgs.RemoveInbox(userID, convID)
}

// buildSingleView 单聊响应视图。
func (h *ConversationHandler) buildSingleView(conv *model.Conversation, target *model.User, meID uint) model.ConversationView {
	peer := target.Brief()
	v := model.ConversationView{
		ID: conv.ID, Type: 1, Peer: &peer, MemberCount: 2,
	}
	if inbox, err := h.msgs.InboxOf(meID, conv.ID); err == nil && inbox != nil {
		v.LastMessageID = inbox.LastMessageID
		v.LastMessageAt = inbox.LastMessageAt
		v.UnreadCount = inbox.UnreadCount
		if inbox.LastMessageID != nil {
			if msg, err := h.msgs.ByID(uint(*inbox.LastMessageID)); err == nil && msg != nil {
				if msg.IsRecalled {
					s := "消息已撤回"
					v.LastMessagePreview = &s
				} else {
					s := msg.Content
					v.LastMessagePreview = &s
				}
			}
		}
	}
	return v
}

// ---- 通用小工具（包内共享） ----

func parseConvID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "会话 id 不合法")
		return 0, false
	}
	return uint(id), true
}

func parseUint64(s string) uint64 {
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}

func parseUint64Default(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func without(ids []uint, exclude uint) []uint {
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id != exclude {
			out = append(out, id)
		}
	}
	return out
}
