package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// FriendHandler 好友关系。
type FriendHandler struct {
	users   *store.UserStore
	friends *store.FriendStore
	convs   *store.ConversationStore
}

func NewFriendHandler(users *store.UserStore, friends *store.FriendStore, convs *store.ConversationStore) *FriendHandler {
	return &FriendHandler{users: users, friends: friends, convs: convs}
}

// List 我的好友。
func (h *FriendHandler) List(c *gin.Context) {
	rows, err := h.friends.AcceptedByUser(middleware.UserFrom(c).ID)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// Requests 收到的好友申请。
func (h *FriendHandler) Requests(c *gin.Context) {
	rows, err := h.friends.PendingToUser(middleware.UserFrom(c).ID)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// Apply 发好友申请；拒绝后可重新申请（复活原记录）。
func (h *FriendHandler) Apply(c *gin.Context) {
	var req struct {
		ToUsername string `json:"toUsername"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	target, err := h.users.ByUsername(req.ToUsername)
	if err != nil {
		internal(c, err)
		return
	}
	if target == nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if target.ID == me.ID {
		fail(c, http.StatusUnprocessableEntity, "不能添加自己为好友")
		return
	}

	// 已有待申请 / 已通过：拒绝重复
	rows, err := h.friends.PendingBetween(me.ID, target.ID, 0)
	if err != nil {
		internal(c, err)
		return
	}
	if len(rows) > 0 {
		fail(c, http.StatusConflict, "对方已是好友或申请中")
		return
	}
	accepted, err := h.friends.PendingBetween(me.ID, target.ID, 1)
	if err != nil {
		internal(c, err)
		return
	}
	if len(accepted) > 0 {
		fail(c, http.StatusConflict, "对方已是好友或申请中")
		return
	}

	// 曾被拒绝：复活原记录（以"我->对方"方向）
	rejected, err := h.friends.PendingBetween(me.ID, target.ID, 2)
	if err != nil {
		internal(c, err)
		return
	}
	if len(rejected) > 0 {
		rejected[0].Status = 0
		if err := h.friends.Save(&rejected[0]); err != nil {
			internal(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	f := &model.Friend{UserID: me.ID, FriendID: target.ID, Status: 0}
	if err := h.friends.Create(f); err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Handle 同意 / 拒绝收到的申请。
func (h *FriendHandler) Handle(c *gin.Context) {
	var req struct {
		RequestID uint `json:"requestId"`
		Accept    bool `json:"accept"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)

	f, err := h.friends.ByID(req.RequestID)
	if err != nil {
		internal(c, err)
		return
	}
	if f == nil || f.FriendID != me.ID || f.Status != 0 {
		fail(c, http.StatusNotFound, "申请不存在或已处理")
		return
	}

	if !req.Accept {
		f.Status = 2
		if err := h.friends.Save(f); err != nil {
			internal(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	// 同意：本记录 0→1，再补一条反向记录
	f.Status = 1
	if err := h.friends.Save(f); err != nil {
		internal(c, err)
		return
	}
	reverse := &model.Friend{UserID: me.ID, FriendID: f.UserID, Status: 1}
	if err := h.friends.Create(reverse); err != nil {
		internal(c, err)
		return
	}

	// 双方各自若无单聊则保持未创建（首次发消息/点开时由前端 createSingle 幂等创建）
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Remove 删除好友（路径参数 = 对方 user_id），双向删除 status=1 记录，单聊保留。
func (h *FriendHandler) Remove(c *gin.Context) {
	friendID, err := strconv.ParseUint(c.Param("friendId"), 10, 64)
	if err != nil || friendID == 0 {
		fail(c, http.StatusBadRequest, "路径参数不合法")
		return
	}
	me := middleware.UserFrom(c)
	if uint(friendID) == me.ID {
		fail(c, http.StatusUnprocessableEntity, "不能删除自己")
		return
	}
	if err := h.friends.DeleteBetween(me.ID, uint(friendID)); err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
