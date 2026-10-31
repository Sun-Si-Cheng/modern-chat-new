package handler

import (
	"crypto/rand"
	"log/slog"
	"math/big"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// AdminHandler 站长后台。
type AdminHandler struct {
	users   *store.UserStore
	convs   *store.ConversationStore
	msgs    *store.MessageStore
	plugins *store.PluginStore
}

// adminStartedAt 进程启动时间，用于概览里的运行时长。
var adminStartedAt = time.Now()

func NewAdminHandler(
	users *store.UserStore,
	convs *store.ConversationStore,
	msgs *store.MessageStore,
	plugins *store.PluginStore,
) *AdminHandler {
	return &AdminHandler{users: users, convs: convs, msgs: msgs, plugins: plugins}
}

// Users 用户分页列表，支持关键词 / 角色 / 封禁状态筛选。
func (h *AdminHandler) Users(c *gin.Context) {
	page := int(parseUint64Default(c.Query("page"), 1))
	pageSize := int(parseUint64Default(c.Query("pageSize"), 20))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	filter := store.AdminUserFilter{
		Keyword: strings.TrimSpace(c.Query("kw")),
		Role:    c.Query("role"),
		Status:  c.Query("status"),
	}
	rows, total, err := h.users.ListFiltered(filter, page, pageSize)
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

// Stats 后台概览：用户 / 会话 / 消息 / 插件 / 在线数 / 运行时长。
func (h *AdminHandler) Stats(c *gin.Context) {
	usersTotal, err := h.users.Count()
	if err != nil {
		internal(c, err)
		return
	}
	usersToday, err := h.users.CountToday()
	if err != nil {
		internal(c, err)
		return
	}
	usersBanned, err := h.users.CountBanned()
	if err != nil {
		internal(c, err)
		return
	}
	admins, err := h.users.CountByRole("admin")
	if err != nil {
		internal(c, err)
		return
	}
	single, group, err := h.convs.CountByType()
	if err != nil {
		internal(c, err)
		return
	}
	msgsTotal, err := h.msgs.Count()
	if err != nil {
		internal(c, err)
		return
	}
	msgsToday, err := h.msgs.CountToday()
	if err != nil {
		internal(c, err)
		return
	}
	installed, enabled, disabled, err := h.plugins.AdminStats()
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"users": gin.H{
			"total":  usersTotal,
			"today":  usersToday,
			"banned": usersBanned,
			"admins": admins,
		},
		"conversations": gin.H{"single": single, "group": group},
		"messages":      gin.H{"total": msgsTotal, "today": msgsToday},
		"plugins":       gin.H{"installed": installed, "enabled": enabled, "disabled": disabled},
		"online":        notifyFor(c).OnlineCount(),
		"uptimeSeconds": int64(time.Since(adminStartedAt).Seconds()),
		"goVersion":     runtime.Version(),
	})
}

// UserDetail 单个用户详情：基础资料 + 会话 / 消息 / 插件计数 + 是否在线。
func (h *AdminHandler) UserDetail(c *gin.Context) {
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
	convs, err := h.convs.CountForUser(id)
	if err != nil {
		internal(c, err)
		return
	}
	msgs, err := h.msgs.CountBySender(id)
	if err != nil {
		internal(c, err)
		return
	}
	plugins, err := h.plugins.CountByUser(id)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user": u,
		"stats": gin.H{
			"conversations": convs,
			"messages":      msgs,
			"plugins":       plugins,
		},
		"online": notifyFor(c).Online(id),
	})
}

// Kick 强制下线但不封禁：断开既有连接，用户可立刻重新登录。
func (h *AdminHandler) Kick(c *gin.Context) {
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
	online := notifyFor(c).Online(id)
	notifyFor(c).KickUser(id, "你已被管理员强制下线")
	slog.Warn("user kicked by admin",
		"target", u.ID, "username", u.Username, "wasOnline", online,
		"by", middleware.UserFrom(c).ID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "wasOnline": online})
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
	// 修复缺陷：此前只推 kicked 帧，客户端忽略该帧即可继续使用既有连接；
	// 改为 KickUser —— 先入帧再强制断开，封禁立即生效且不可 bypass。
	notifyFor(c).KickUser(u.ID, "账号已被封禁")
	slog.Warn("user banned",
		"target", u.ID, "username", u.Username,
		"days", req.Days, "until", until.Format(time.RFC3339), "reason", req.Reason,
		"by", middleware.UserFrom(c).ID)
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
	slog.Info("user unbanned", "target", u.ID, "username", u.Username, "by", middleware.UserFrom(c).ID)
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
	// 修复缺陷：重置密码使 tv+1，但旧连接不会自动失效；改用 KickUser 强制断开。
	notifyFor(c).KickUser(u.ID, "密码已被站长重置")
	slog.Warn("user password reset by admin",
		"target", u.ID, "username", u.Username, "by", middleware.UserFrom(c).ID)
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
	slog.Warn("plugin globally toggled",
		"name", req.Name, "disabled", req.Disabled, "affected", affected,
		"by", middleware.UserFrom(c).ID)
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
