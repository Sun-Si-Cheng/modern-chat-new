package handler

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// AdminHandler 站长后台。
type AdminHandler struct {
	users   *store.UserStore
	convs   *store.ConversationStore
	plugins *store.PluginStore
}

func NewAdminHandler(users *store.UserStore, convs *store.ConversationStore, plugins *store.PluginStore) *AdminHandler {
	return &AdminHandler{users: users, convs: convs, plugins: plugins}
}

// Users 用户分页列表。
func (h *AdminHandler) Users(c *gin.Context) {
	page := int(parseUint64Default(c.Query("page"), 1))
	pageSize := int(parseUint64Default(c.Query("pageSize"), 20))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	total, err := h.users.Count()
	if err != nil {
		internal(c, err)
		return
	}
	rows, err := h.users.List(page, pageSize)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"users":    rows,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// Ban 封禁：days=0 永久（实现为 100 年）；tv+1 并强制下线。
func (h *AdminHandler) Ban(c *gin.Context) {
	id, ok := parseUserID(c)
	if !ok {
		return
	}
	var req struct {
		Days   int    `json:"days"`
		Reason string `json:"reason"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Days < 0 || req.Days > 3650 {
		fail(c, http.StatusBadRequest, "days 取值 0-3650（0 表示永久）")
		return
	}
	u, err := h.users.ByID(id)
	if err != nil {
		internal(c, err)
		return
	}
	if u == nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if u.Role == "admin" {
		fail(c, http.StatusForbidden, "不能封禁站长账号")
		return
	}

	var until time.Time
	if req.Days == 0 {
		until = time.Now().AddDate(100, 0, 0)
	} else {
		until = time.Now().AddDate(0, 0, req.Days)
	}
	u.BannedUntil = &until
	if req.Reason != "" {
		r := req.Reason
		u.BanReason = &r
	}
	u.TokenVersion++
	if err := h.users.Save(u); err != nil {
		internal(c, err)
		return
	}
	notifyFor(c).ToUsers([]uint{u.ID}, gin.H{"type": "kicked", "reason": "账号已被封禁"})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Unban 解封。
func (h *AdminHandler) Unban(c *gin.Context) {
	id, ok := parseUserID(c)
	if !ok {
		return
	}
	u, err := h.users.ByID(id)
	if err != nil {
		internal(c, err)
		return
	}
	if u == nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	u.BannedUntil = nil
	u.BanReason = nil
	if err := h.users.Save(u); err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ResetPassword 生成 12 位随机密码；tv+1 强制下线。
func (h *AdminHandler) ResetPassword(c *gin.Context) {
	id, ok := parseUserID(c)
	if !ok {
		return
	}
	u, err := h.users.ByID(id)
	if err != nil {
		internal(c, err)
		return
	}
	if u == nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	pwd := randomPassword(12)
	hash, err := hashPassword(pwd)
	if err != nil {
		internal(c, err)
		return
	}
	u.PasswordHash = hash
	u.TokenVersion++
	if err := h.users.Save(u); err != nil {
		internal(c, err)
		return
	}
	notifyFor(c).ToUsers([]uint{u.ID}, gin.H{"type": "kicked", "reason": "密码已被站长重置"})
	c.JSON(http.StatusOK, gin.H{"newPassword": pwd})
}

// Plugins 插件聚合列表（按 name，含全局下架状态）。
func (h *AdminHandler) Plugins(c *gin.Context) {
	page := int(parseUint64Default(c.Query("page"), 1))
	pageSize := int(parseUint64Default(c.Query("pageSize"), 20))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	rows, total, err := h.plugins.AdminList(page, pageSize)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"plugins":  rows,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// SetPluginGlobalDisabled 按插件名全局下架 / 恢复（对所有用户立即生效）。
func (h *AdminHandler) SetPluginGlobalDisabled(c *gin.Context) {
	var req struct {
		Name     string `json:"name"`
		Disabled bool   `json:"disabled"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Name == "" || len([]rune(req.Name)) > 30 {
		fail(c, http.StatusBadRequest, "插件名不合法")
		return
	}
	affected, err := h.plugins.SetGlobalDisabledByName(req.Name, req.Disabled)
	if err != nil {
		internal(c, err)
		return
	}
	if affected == 0 {
		fail(c, http.StatusNotFound, "插件不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "affected": affected})
}

func parseUserID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "用户 id 不合法")
		return 0, false
	}
	return uint(id), true
}

const passwordAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// randomPassword 生成 n 位随机密码（去除易混字符）。
func randomPassword(n int) string {
	out := make([]byte, n)
	max := big.NewInt(int64(len(passwordAlphabet)))
	for i := range out {
		r, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic("crypto/rand unavailable: " + err.Error())
		}
		out[i] = passwordAlphabet[r.Int64()]
	}
	return string(out)
}
