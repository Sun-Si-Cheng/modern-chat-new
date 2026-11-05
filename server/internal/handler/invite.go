package handler

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// InviteHandler 注册邀请码管理（仅站长）。
type InviteHandler struct {
	invites *store.InviteStore
}

func NewInviteHandler(invites *store.InviteStore) *InviteHandler {
	return &InviteHandler{invites: invites}
}

// List 列出邀请码（新→旧）。
func (h *InviteHandler) List(c *gin.Context) {
	rows, err := h.invites.List(100)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// Create 生成一批邀请码。
func (h *InviteHandler) Create(c *gin.Context) {
	var req struct {
		MaxUses     int `json:"maxUses"`
		ExpiresDays int `json:"expiresDays"`
		Count       int `json:"count"`
	}
	if !bind(c, &req) {
		return
	}
	if req.MaxUses < 1 || req.MaxUses > 1000 {
		fail(c, http.StatusBadRequest, "可用次数需为 1-1000")
		return
	}
	if req.ExpiresDays < 0 || req.ExpiresDays > 365 {
		fail(c, http.StatusBadRequest, "有效期需为 0（永久）-365 天")
		return
	}
	if req.Count < 1 || req.Count > 20 {
		fail(c, http.StatusBadRequest, "一次最多生成 20 个")
		return
	}

	var expiresAt *time.Time
	if req.ExpiresDays > 0 {
		t := time.Now().AddDate(0, 0, req.ExpiresDays)
		expiresAt = &t
	}
	me := middleware.UserFrom(c)
	out := make([]model.InviteCode, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		code, err := randomInviteCode()
		if err != nil {
			internal(c, err)
			return
		}
		row := &model.InviteCode{
			Code:      code,
			CreatedBy: me.ID,
			MaxUses:   req.MaxUses,
			ExpiresAt: expiresAt,
		}
		row.Creator = me.Nickname
		if err := h.invites.Create(row); err != nil {
			internal(c, err)
			return
		}
		out = append(out, *row)
	}
	slog.Info("invite codes created", "by", me.ID, "count", len(out))
	c.JSON(http.StatusOK, out)
}

// SetDisabled 停用 / 启用一个邀请码（不删除，保留审计线索）。
func (h *InviteHandler) SetDisabled(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "邀请码 id 不合法")
		return
	}
	var req struct {
		Disabled bool `json:"disabled"`
	}
	if !bind(c, &req) {
		return
	}
	ok, err := h.invites.Disable(uint(id), req.Disabled)
	if err != nil {
		internal(c, err)
		return
	}
	if !ok {
		fail(c, http.StatusNotFound, "邀请码不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// randomInviteCode 12 字节随机数（96 bit），hex 后 24 位。
// 不用 math/rand：邀请码是凭据，必须不可预测。
func randomInviteCode() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
