package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// UserHandler 用户搜索。
type UserHandler struct {
	users *store.UserStore
}

func NewUserHandler(users *store.UserStore) *UserHandler { return &UserHandler{users: users} }

// Search 搜人（摘要，不含 email）。
func (h *UserHandler) Search(c *gin.Context) {
	kw := c.Query("kw")
	if len([]rune(kw)) == 0 || len([]rune(kw)) > 50 {
		fail(c, http.StatusBadRequest, "关键词需为 1-50 个字符")
		return
	}
	rows, err := h.users.Search(kw, 20)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// LogoutOthers 让该账号的所有登录态失效（token_version+1）。
//
// 当前 JWT 是无状态的，没有「单条会话」标识，因此这里只能一次性让全部设备
// 重新登录。对用户表现为「退出所有设备」，对安全场景（怀疑令牌泄露）够用。
func (h *UserHandler) LogoutOthers(c *gin.Context) {
	me := middleware.UserFrom(c)
	if err := h.users.BumpTokenVersion(me.ID); err != nil {
		internal(c, err)
		return
	}
	// 仅 bump token_version 不会影响已建立的 WS 连接（它们只缓存握手时的 tv），
	// 必须显式踢掉，否则旧连接可以继续收发消息直到自然断开。
	notifyFor(c).KickUser(me.ID, "已退出所有设备，请重新登录")
	slog.Info("logout all devices", "uid", me.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
