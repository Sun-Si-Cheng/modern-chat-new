package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

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
