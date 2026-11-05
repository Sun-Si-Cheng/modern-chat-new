package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// 群成员上限见 store.MaxGroupMembers（§3.2）。

// ConversationHandler 会话与消息 REST。
type ConversationHandler struct {
	users   *store.UserStore
	friends *store.FriendStore
	convs   *store.ConversationStore
	msgs    *store.MessageStore
	// mod 拉黑关系：单聊发起前双向拦截
	mod *store.ModerationStore
	// settings 用于读取群人数上限等可运营规则（可为 nil，回落到内置默认值）
	settings *store.SettingStore
	// storage 校验群头像 URL 归属（可为 nil，回落本地前缀判断）
	storage service.Storage
}

// maxGroupMembers 当前生效的群人数上限。
func (h *ConversationHandler) maxGroupMembers() int {
	if h.settings == nil {
		return store.DefaultOps().MaxGroupMembers
	}
	return h.settings.Ops().MaxGroupMembers
}

func NewConversationHandler(
	users *store.UserStore,
	friends *store.FriendStore,
	convs *store.ConversationStore,
	msgs *store.MessageStore,
	mod *store.ModerationStore,
	settings *store.SettingStore,
	storage service.Storage,
) *ConversationHandler {
	return &ConversationHandler{
		users: users, friends: friends, convs: convs, msgs: msgs, mod: mod,
		settings: settings, storage: storage,
	}
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
	// 拉黑即互不打扰：任一方向拉黑都不允许发起（或复用）单聊
	if h.mod != nil {
		if blocked, err := h.mod.BlockedEither(me.ID, target.ID); err != nil {
			internal(c, err)
			return
		} else if blocked {
			fail(c, http.StatusForbidden, "对方已将你拉黑，无法发起会话")
			return
		}
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
	notifyFor(c).ToUsers([]uint{target.ID}, convMetaFrame(conv.ID))
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
	afterID := parseUint64(c.Query("afterId"))
	maxLimit := 50
	if afterID > 0 {
		// 增量补齐一次可以多取一些，减少离线恢复时的往返次数
		maxLimit = 200
	}
	if limit < 1 || limit > maxLimit {
		limit = 30
	}
	var rows []model.Message
	var hasMore bool
	var err error
	if afterID > 0 {
		// 增量补齐：只取 afterId 之后的消息（升序），用于离线/断线场景
		rows, hasMore, err = h.msgs.AfterID(convID, middleware.UserFrom(c).ID, afterID, limit)
	} else {
		rows, hasMore, err = h.msgs.PageBefore(convID, middleware.UserFrom(c).ID, beforeID, limit)
	}
	if err != nil {
		internal(c, err)
		return
	}
	// 空会话时 rows 可能是 nil → 序列化成 "messages": null，
	// 前端拿到后 .slice() 会崩（打开空会话直接白屏）。统一给空数组。
	if rows == nil {
		rows = []model.Message{}
	}
	// 表情回应是聚合数据，随历史分页一并下发，避免前端逐条请求
	if err := h.msgs.AttachReactions(rows, middleware.UserFrom(c).ID); err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": rows, "hasMore": hasMore})
}

// DeleteMessage 仅自己删除：只从调用者的聊天记录移除，对方与其它成员仍可见。
func (h *ConversationHandler) DeleteMessage(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	msgID, ok := parseMessageID(c)
	if !ok {
		return
	}
	if !h.requireMember(c, convID) {
		return
	}
	msg, err := h.msgs.ByID(msgID)
	if err != nil {
		internal(c, err)
		return
	}
	if msg == nil || msg.ConversationID != convID {
		fail(c, http.StatusNotFound, "消息不存在")
		return
	}
	me := middleware.UserFrom(c)
	if err := h.msgs.DeleteForUser(me.ID, msg); err != nil {
		internal(c, err)
		return
	}
	slog.Info("message deleted for user", "msg", msgID, "conv", convID, "uid", me.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Favorite 收藏消息。
func (h *ConversationHandler) Favorite(c *gin.Context) {
	msg, ok := h.ownedMessage(c)
	if !ok {
		return
	}
	me := middleware.UserFrom(c)
	if err := h.msgs.Favorite(me.ID, msg.ID); err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Unfavorite 取消收藏。
func (h *ConversationHandler) Unfavorite(c *gin.Context) {
	msg, ok := h.ownedMessage(c)
	if !ok {
		return
	}
	me := middleware.UserFrom(c)
	if err := h.msgs.Unfavorite(me.ID, msg.ID); err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// SearchMessages 消息搜索。convId 可选：传了就是会话内搜索，不传是全局搜索。
func (h *ConversationHandler) SearchMessages(c *gin.Context) {
	kw := strings.TrimSpace(c.Query("kw"))
	if n := len([]rune(kw)); n < 1 || n > 50 {
		fail(c, http.StatusBadRequest, "关键词需为 1-50 个字符")
		return
	}
	convID := uint(parseUint64(c.Query("convId")))
	if convID > 0 && !h.requireMember(c, convID) {
		return
	}
	limit := parseUint64Default(c.Query("limit"), 30)
	if limit < 1 || limit > 100 {
		limit = 30
	}
	rows, err := h.msgs.Search(middleware.UserFrom(c).ID, convID, kw, limit)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// Favorites 收藏列表（按收藏时间倒序）。
func (h *ConversationHandler) Favorites(c *gin.Context) {
	limit := parseUint64Default(c.Query("limit"), 100)
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := h.msgs.Favorites(middleware.UserFrom(c).ID, limit)
	if err != nil {
		internal(c, err)
		return
	}
	// 兜底：nil slice 会被序列化成 null（客户端 `.map` 会崩），统一给空数组
	if rows == nil {
		rows = []model.FavoriteView{}
	}
	c.JSON(http.StatusOK, rows)
}

// ownedMessage 取路径里的消息并校验调用者是该会话成员。
func (h *ConversationHandler) ownedMessage(c *gin.Context) (*model.Message, bool) {
	msgID, ok := parseMessageID(c)
	if !ok {
		return nil, false
	}
	msg, err := h.msgs.ByID(msgID)
	if err != nil {
		internal(c, err)
		return nil, false
	}
	if msg == nil {
		fail(c, http.StatusNotFound, "消息不存在")
		return nil, false
	}
	if !h.requireMember(c, msg.ConversationID) {
		return nil, false
	}
	return msg, true
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
	maxMembers := int64(h.maxGroupMembers())
	if err := h.convs.JoinGroup(convID, me.ID, maxMembers); err != nil {
		if errors.Is(err, store.ErrGroupFull) {
			fail(c, http.StatusUnprocessableEntity,
				fmt.Sprintf("群成员已满（%d 人上限）", maxMembers))
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
	notifyFor(c).ToUsers(ids, convMetaFrame(convID))
	slog.Info("conversation joined", "conv", convID, "uid", me.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Invite 拉好友入群：任意群成员都可以邀请，但只能邀请自己的好友。
// 已在群中的人会被忽略（幂等），整批超出人数上限则整体拒绝。
func (h *ConversationHandler) Invite(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	var req struct {
		UserIDs []uint `json:"userIds"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	if !h.requireMember(c, convID) {
		return
	}
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
		fail(c, http.StatusUnprocessableEntity, "只有群聊可以拉人")
		return
	}

	// 去重并排除自己
	ids := make([]uint, 0, len(req.UserIDs))
	seen := make(map[uint]bool, len(req.UserIDs))
	for _, id := range req.UserIDs {
		if id == 0 || id == me.ID || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		fail(c, http.StatusBadRequest, "请选择要邀请的好友")
		return
	}
	if len(ids) > 50 {
		fail(c, http.StatusBadRequest, "一次最多邀请 50 人")
		return
	}

	// 只能拉自己的好友：避免任意用户被陌生人强行拉进群
	friendIDs, err := h.friends.FriendIDs(me.ID)
	if err != nil {
		internal(c, err)
		return
	}
	isFriend := make(map[uint]bool, len(friendIDs))
	for _, id := range friendIDs {
		isFriend[id] = true
	}
	for _, id := range ids {
		if !isFriend[id] {
			fail(c, http.StatusUnprocessableEntity, "只能邀请自己的好友")
			return
		}
	}

	maxMembers := int64(h.maxGroupMembers())
	added, err := h.convs.InviteMembers(convID, ids, maxMembers)
	if err != nil {
		if errors.Is(err, store.ErrGroupFull) {
			fail(c, http.StatusUnprocessableEntity,
				fmt.Sprintf("群成员已满（%d 人上限）", maxMembers))
			return
		}
		internal(c, err)
		return
	}
	if len(added) > 0 {
		if err := h.initInboxes(convID, added...); err != nil {
			internal(c, err)
			return
		}
	}
	allIDs, err := h.convs.MemberUserIDs(convID)
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("members invited", "conv", convID, "by", me.ID, "added", len(added))
	// 新成员也要收到：他们的会话列表需要立刻出现这个群
	notifyFor(c).ToUsers(allIDs, convMetaFrame(convID))
	c.JSON(http.StatusOK, gin.H{"ok": true, "added": len(added)})
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
	notifyFor(c).ToUsers(ids, convMetaFrame(convID))
	slog.Info("conversation left", "conv", convID, "uid", me.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Read 上报已读。
// SetPrefs 会话个人设置：置顶 / 免打扰。
//
// 存在 user_conversations（每人一份），我置顶不代表别人也置顶。
// 字段用指针接收：不传表示「不改这项」，显式 false 才会取消。
func (h *ConversationHandler) SetPrefs(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	var req struct {
		Pinned   *bool `json:"pinned"`
		Muted    *bool `json:"muted"`
		Archived *bool `json:"archived"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Pinned == nil && req.Muted == nil && req.Archived == nil {
		fail(c, http.StatusBadRequest, "没有要修改的设置")
		return
	}
	me := middleware.UserFrom(c)
	if !h.requireMember(c, convID) {
		return
	}
	if err := h.convs.SetPrefs(me.ID, convID, req.Pinned, req.Muted, req.Archived); err != nil {
		internal(c, err)
		return
	}
	pinned, muted, archived, err := h.convs.PrefsOf(me.ID, convID)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "pinned": pinned, "muted": muted, "archived": archived})
}

// Delete 从自己的会话列表移除（不退出群、不删除消息）；之后新消息会重新出现。
func (h *ConversationHandler) Delete(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	me := middleware.UserFrom(c)
	if !h.requireMember(c, convID) {
		return
	}
	if err := h.convs.DeleteInbox(me.ID, convID); err != nil {
		internal(c, err)
		return
	}
	notifyFor(c).ToUsers([]uint{me.ID}, gin.H{"type": "conv_change", "meta": true, "convId": convID})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Clear 清空我在该会话的聊天记录：消息本体保留，仅设置个人水位。
func (h *ConversationHandler) Clear(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	var req struct {
		UpToID uint64 `json:"upToId"`
	}
	// 清空允许空 body（默认清到最新）；只有确实带了 body 才解析
	if c.Request.ContentLength > 0 && !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	if !h.requireMember(c, convID) {
		return
	}
	upTo := req.UpToID
	if upTo == 0 {
		latest, err := h.convs.LatestMessageID(convID)
		if err != nil {
			internal(c, err)
			return
		}
		upTo = latest
	} else {
		msg, err := h.msgs.ByID(uint(upTo))
		if err != nil {
			internal(c, err)
			return
		}
		if msg == nil || msg.ConversationID != convID {
			fail(c, http.StatusBadRequest, "upToId 不合法")
			return
		}
	}
	if err := h.convs.ClearHistory(me.ID, convID, upTo); err != nil {
		internal(c, err)
		return
	}
	notifyFor(c).ToUsers([]uint{me.ID}, gin.H{"type": "conv_change", "meta": true, "convId": convID})
	c.JSON(http.StatusOK, gin.H{"ok": true, "clearedBeforeId": upTo})
}

// SetMuteAll 群全员禁言（群主 / 管理员）。
func (h *ConversationHandler) SetMuteAll(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	var req struct {
		Muted bool `json:"muted"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	if _, ok := h.requireManager(c, convID, me.ID); !ok {
		return
	}
	if err := h.convs.SetMuteAll(convID, req.Muted); err != nil {
		internal(c, err)
		return
	}
	ids, err := h.convs.MemberUserIDs(convID)
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("group mute all changed", "conv", convID, "muted", req.Muted, "by", me.ID)
	notifyFor(c).ToUsers(ids, convMetaFrame(convID))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

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
			// 包括调用的账号自身：同账号其他在线端需要同步清未读
			notifyFor(c).ToUsers(ids, gin.H{
				"type": "read", "convId": convID, "userId": me.ID, "upToId": req.UpToID,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// SetAvatar 修改群头像：群主 / 管理员可操作，空串表示恢复默认头像。
// 头像地址必须来自本站上传接口，防止把群头像换成任意外链（会泄露成员 IP）。
func (h *ConversationHandler) SetAvatar(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	var req struct {
		Avatar string `json:"avatar"`
	}
	if !bind(c, &req) {
		return
	}
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
		fail(c, http.StatusUnprocessableEntity, "只有群聊可以设置头像")
		return
	}
	me := middleware.UserFrom(c)
	if _, ok := h.requireManager(c, convID, me.ID); !ok {
		return
	}
	avatar := strings.TrimSpace(req.Avatar)
	if avatar != "" {
		owned := false
		if h.storage != nil {
			owned = h.storage.Owns(avatar)
		} else {
			owned = strings.HasPrefix(avatar, "/uploads/") && !strings.Contains(avatar, "..")
		}
		if !owned {
			fail(c, http.StatusBadRequest, "头像地址不合法")
			return
		}
	}
	if err := h.convs.SetAvatar(convID, avatar); err != nil {
		internal(c, err)
		return
	}
	ids, err := h.convs.MemberUserIDs(convID)
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("group avatar updated", "conv", convID, "by", me.ID, "empty", avatar == "")
	notifyFor(c).ToUsers(ids, convMetaFrame(convID))
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
	notifyFor(c).ToUsers(ids, convMetaFrame(convID))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// SetAnnouncement 群公告：仅群主可设，传空串表示撤销。
//
// 公告存在 conversations 表上（所有成员共享同一份），
// 与系统公告会话（type=3）不是一回事。
func (h *ConversationHandler) SetAnnouncement(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	conv, err := h.convs.ByID(convID)
	if err != nil {
		internal(c, err)
		return
	}
	if conv == nil || conv.Type != 2 {
		fail(c, http.StatusBadRequest, "只有群聊可以设置公告")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if !bind(c, &req) {
		return
	}
	content := strings.TrimSpace(req.Content)
	if n := len([]rune(content)); n > 500 {
		fail(c, http.StatusBadRequest, "公告最多 500 字")
		return
	}
	me := middleware.UserFrom(c)
	if !h.requireOwner(c, convID, me.ID) {
		return
	}
	if err := h.convs.SetAnnouncement(convID, me.ID, content); err != nil {
		internal(c, err)
		return
	}
	slog.Info("group announcement updated", "conv", convID, "by", me.ID, "len", len([]rune(content)))

	// 通知所有成员刷新（公告显示在聊天页顶部，实时生效比等下拉刷新自然）
	ids, err := h.convs.MemberUserIDs(convID)
	if err != nil {
		internal(c, err)
		return
	}
	notifyFor(c).ToUsers(ids, convMetaFrame(convID))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Kick 群主 / 管理员移出成员。
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
	caller, ok := h.requireManager(c, convID, me.ID)
	if !ok {
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
	} else if m.Role == "owner" {
		// 修复缺陷：规格 §3.2 要求群主不可被移除；原实现只挡了「移除自己」，
		// 若数据里出现第二个 owner 角色行（转让竞态等），群主仍可能被踢掉。
		fail(c, http.StatusUnprocessableEntity, "不能移除群主")
	} else if caller.Role == "admin" && m.Role == "admin" {
		fail(c, http.StatusForbidden, "管理员不能移除其他管理员")
	} else {
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
		notifyFor(c).ToUsers(append(ids, req.UserID), convMetaFrame(convID))
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// SetAdmin 群主设置 / 取消管理员。
func (h *ConversationHandler) SetAdmin(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	var req struct {
		UserID uint `json:"userId"`
		Admin  bool `json:"admin"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	if !h.requireOwner(c, convID, me.ID) {
		return
	}
	if req.UserID == me.ID {
		fail(c, http.StatusUnprocessableEntity, "不能调整自己的角色")
		return
	}
	m, err := h.convs.MemberOf(convID, req.UserID)
	if err != nil {
		internal(c, err)
		return
	}
	if m == nil {
		fail(c, http.StatusUnprocessableEntity, "该用户不在群中")
		return
	}
	if m.Role == "owner" {
		fail(c, http.StatusUnprocessableEntity, "不能调整群主")
		return
	}
	role := "member"
	if req.Admin {
		role = "admin"
	}
	if err := h.convs.SetMemberRole(convID, req.UserID, role); err != nil {
		internal(c, err)
		return
	}
	slog.Info("group admin changed", "conv", convID, "target", req.UserID, "role", role, "by", me.ID)
	ids, err := h.convs.MemberUserIDs(convID)
	if err != nil {
		internal(c, err)
		return
	}
	notifyFor(c).ToUsers(ids, convMetaFrame(convID))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Mute 群主 / 管理员禁言成员。minutes=0 表示解除禁言。
func (h *ConversationHandler) Mute(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	var req struct {
		UserID  uint `json:"userId"`
		Minutes int  `json:"minutes"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	caller, ok := h.requireManager(c, convID, me.ID)
	if !ok {
		return
	}
	if req.UserID == me.ID {
		fail(c, http.StatusUnprocessableEntity, "不能禁言自己")
		return
	}
	if req.Minutes < 0 || req.Minutes > 43200 {
		fail(c, http.StatusBadRequest, "禁言时长需为 0-43200 分钟")
		return
	}
	m, err := h.convs.MemberOf(convID, req.UserID)
	if err != nil {
		internal(c, err)
		return
	}
	if m == nil {
		fail(c, http.StatusUnprocessableEntity, "该用户不在群中")
		return
	}
	if m.Role == "owner" {
		fail(c, http.StatusUnprocessableEntity, "不能禁言群主")
		return
	}
	if caller.Role == "admin" && m.Role == "admin" {
		fail(c, http.StatusForbidden, "管理员不能禁言其他管理员")
		return
	}
	var until *time.Time
	if req.Minutes > 0 {
		t := time.Now().Add(time.Duration(req.Minutes) * time.Minute)
		until = &t
	}
	if err := h.convs.SetMute(convID, req.UserID, until); err != nil {
		internal(c, err)
		return
	}
	slog.Info("group member mute changed", "conv", convID, "target", req.UserID, "minutes", req.Minutes, "by", me.ID)
	ids, err := h.convs.MemberUserIDs(convID)
	if err != nil {
		internal(c, err)
		return
	}
	notifyFor(c).ToUsers(ids, convMetaFrame(convID))
	resp := gin.H{"ok": true}
	if until != nil {
		resp["mutedUntil"] = until.UTC().Format(time.RFC3339Nano)
	}
	c.JSON(http.StatusOK, resp)
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
	notifyFor(c).ToUsers(ids, convMetaFrame(convID))
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

// requireManager 群主或管理员；返回调用者的成员行（用于进一步判断权限边界）。
func (h *ConversationHandler) requireManager(c *gin.Context, convID, userID uint) (*model.ConversationMember, bool) {
	if !h.requireMember(c, convID) {
		return nil, false
	}
	m, err := h.convs.MemberOf(convID, userID)
	if err != nil {
		internal(c, err)
		return nil, false
	}
	if m == nil || (m.Role != "owner" && m.Role != "admin") {
		fail(c, http.StatusForbidden, "无权执行此操作")
		return nil, false
	}
	return m, true
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

func parseMessageID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "消息 id 不合法")
		return 0, false
	}
	return uint(id), true
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
