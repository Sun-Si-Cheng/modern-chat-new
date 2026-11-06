package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// FriendHandler 好友关系。
type FriendHandler struct {
	users   *store.UserStore
	friends *store.FriendStore
	convs   *store.ConversationStore
	// mod 拉黑关系：被拉黑后不能再发好友申请
	mod *store.ModerationStore
}

func NewFriendHandler(
	users *store.UserStore,
	friends *store.FriendStore,
	convs *store.ConversationStore,
	mod *store.ModerationStore,
) *FriendHandler {
	return &FriendHandler{users: users, friends: friends, convs: convs, mod: mod}
}

// List 我的好友。
func (h *FriendHandler) List(c *gin.Context) {
	rows, err := h.friends.AcceptedByUser(middleware.UserFrom(c).ID)
	if err != nil {
		internal(c, err)
		return
	}
	// nil slice 会序列化成 null，客户端 `.filter/.map` 直接抛错；统一给空数组
	if rows == nil {
		rows = []model.FriendItemView{}
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
	if rows == nil {
		rows = []model.FriendRequestView{}
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
	if h.mod != nil {
		if blocked, err := h.mod.BlockedEither(me.ID, target.ID); err != nil {
			internal(c, err)
			return
		} else if blocked {
			fail(c, http.StatusForbidden, "无法向对方发送好友申请")
			return
		}
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
		// (user_id, friend_id) 唯一索引挡住并发重复申请：对方已经收到一条，
		// 这里按「已是好友或申请中」返回，而不是 500。
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			fail(c, http.StatusConflict, "对方已是好友或申请中")
			return
		}
		internal(c, err)
		return
	}
	slog.Info("friend request sent", "from", me.ID, "to", target.ID)
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
		slog.Info("friend request rejected", "from", f.UserID, "by", me.ID)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	// 同意：本记录 0→1，再补一条反向记录
	f.Status = 1
	if err := h.friends.Save(f); err != nil {
		internal(c, err)
		return
	}
	if err := h.friends.EnsureAccepted(me.ID, f.UserID); err != nil {
		internal(c, err)
		return
	}
	// 收尾：把这对之间其它的待处理记录置为已拒绝。
	// 两人互发过申请（并发竞态）时会留下第二条 status=0，不清掉的话
	// 对方会一直看到「好友申请」，同时又出现在「我的好友」里 —— 即重复好友。
	if err := h.friends.ClosePendingBetween(f.UserID, me.ID, f.ID); err != nil {
		slog.Warn("friend pending cleanup failed", "a", f.UserID, "b", me.ID, "err", err.Error())
	}
	slog.Info("friend request accepted", "from", f.UserID, "by", me.ID)

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
	slog.Info("friend removed", "user", me.ID, "friend", friendID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
