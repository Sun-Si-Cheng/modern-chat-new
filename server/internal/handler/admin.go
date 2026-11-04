package handler

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// AdminHandler 站长后台。
type AdminHandler struct {
	users    *store.UserStore
	convs    *store.ConversationStore
	msgs     *store.MessageStore
	plugins  *store.PluginStore
	audits   *store.AuditStore
	settings *store.SettingStore
	mailer   *service.Mailer
	cfg      config.Config
}

// adminStartedAt 进程启动时间，用于概览里的运行时长。
var adminStartedAt = time.Now()

func NewAdminHandler(
	cfg config.Config,
	users *store.UserStore,
	convs *store.ConversationStore,
	msgs *store.MessageStore,
	plugins *store.PluginStore,
	audits *store.AuditStore,
	settings *store.SettingStore,
	mailer *service.Mailer,
) *AdminHandler {
	return &AdminHandler{
		users: users, convs: convs, msgs: msgs, plugins: plugins,
		audits: audits, settings: settings, mailer: mailer, cfg: cfg,
	}
}

// audit 记录一条站长操作。写失败只打日志，不影响主流程。
func (h *AdminHandler) audit(c *gin.Context, action, targetType string, targetID uint, targetName, detail string) {
	admin := middleware.UserFrom(c)
	if admin == nil {
		return
	}
	if err := h.audits.Record(store.AuditEntry{
		AdminID:    admin.ID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		TargetName: targetName,
		Detail:     detail,
		IP:         c.ClientIP(),
	}); err != nil {
		slog.Warn("admin audit write failed", "action", action, "target", targetID, "err", err.Error())
	}
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
	h.audit(c, "kick", "user", u.ID, u.Username, fmt.Sprintf("wasOnline=%v", online))
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
	// 重新封禁时若没填原因，清掉上一次的旧原因（否则会显示与本次封禁无关的说明）
	if req.Reason != "" {
		r := req.Reason
		u.BanReason = &r
	} else {
		u.BanReason = nil
	}
	u.TokenVersion++
	if err := h.users.Save(u); err != nil {
		internal(c, err)
		return
	}
	// 修复缺陷：此前只推 kicked 帧，客户端忽略该帧即可继续使用既有连接；
	// 改为 KickUser —— 先入帧再强制断开，封禁立即生效且不可 bypass。
	notifyFor(c).KickUser(u.ID, "账号已被封禁")
	h.audit(c, "ban", "user", u.ID, u.Username,
		fmt.Sprintf("days=%d until=%s reason=%s", req.Days, until.Format(time.RFC3339), req.Reason))
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
	h.audit(c, "unban", "user", u.ID, u.Username, "")
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
	h.audit(c, "reset_password", "user", u.ID, u.Username, "")
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
	action := "restore_plugin"
	if req.Disabled {
		action = "disable_plugin"
	}
	h.audit(c, action, "plugin", 0, req.Name, fmt.Sprintf("affected=%d", affected))
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

// SetRole 调整用户角色（设 / 取消站长）。禁止把最后一个站长降级。
func (h *AdminHandler) SetRole(c *gin.Context) {
	id, ok := parseUserID(c)
	if !ok {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Role != "admin" && req.Role != "user" {
		fail(c, http.StatusBadRequest, "role 只能是 admin 或 user")
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
	if u.Role == req.Role {
		c.JSON(http.StatusOK, gin.H{"ok": true, "changed": false})
		return
	}
	if u.Role == "admin" {
		n, err := h.users.CountByRole("admin")
		if err != nil {
			internal(c, err)
			return
		}
		if n <= 1 {
			fail(c, http.StatusForbidden, "至少需要保留一个站长")
			return
		}
	}
	u.Role = req.Role
	if err := h.users.Save(u); err != nil {
		internal(c, err)
		return
	}
	// 鉴权中间件每次请求都按 id 查库，角色变更立即生效，无需 token_version+1
	h.audit(c, "set_role", "user", u.ID, u.Username, "role="+req.Role)
	slog.Warn("user role changed",
		"target", u.ID, "username", u.Username, "role", req.Role, "by", middleware.UserFrom(c).ID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "changed": true})
}

// PurgePlugin 按插件名强制卸载（userId=0 表示所有用户）。
func (h *AdminHandler) PurgePlugin(c *gin.Context) {
	var req struct {
		Name   string `json:"name"`
		UserID uint   `json:"userId"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Name == "" || len([]rune(req.Name)) > 30 {
		fail(c, http.StatusBadRequest, "插件名不合法")
		return
	}
	affected, err := h.plugins.PurgeByName(req.Name, req.UserID)
	if err != nil {
		internal(c, err)
		return
	}
	if affected == 0 {
		fail(c, http.StatusNotFound, "没有匹配的插件安装记录")
		return
	}
	h.audit(c, "purge_plugin", "plugin", req.UserID, req.Name,
		fmt.Sprintf("affected=%d userId=%d", affected, req.UserID))
	slog.Warn("plugin purged",
		"name", req.Name, "userId", req.UserID, "affected", affected, "by", middleware.UserFrom(c).ID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "affected": affected})
}

// Announce 全站公告：写入系统会话（type=3）并向所有用户推送。
//
// 系统会话首次发送时创建，并把当时所有用户加为成员；之后新注册用户
// 在下次公告时补入（AddMembersIgnoreDuplicate 幂等）。
func (h *AdminHandler) Announce(c *gin.Context) {
	var req struct {
		Content string `json:"content"`
	}
	if !bind(c, &req) {
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" || len([]rune(content)) > 1000 {
		fail(c, http.StatusBadRequest, "公告内容需为 1-1000 字")
		return
	}
	admin := middleware.UserFrom(c)
	if admin == nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}

	conv, err := h.convs.EnsureSystemConversation("系统通知")
	if err != nil {
		internal(c, err)
		return
	}
	userIDs, err := h.users.AllIDs()
	if err != nil {
		internal(c, err)
		return
	}
	if len(userIDs) == 0 {
		fail(c, http.StatusBadRequest, "还没有用户")
		return
	}

	members := make([]model.ConversationMember, 0, len(userIDs))
	for _, uid := range userIDs {
		members = append(members, model.ConversationMember{
			ConversationID: conv.ID,
			UserID:         uid,
			Role:           "member",
		})
	}
	if err := h.convs.AddMembersIgnoreDuplicate(members); err != nil {
		internal(c, err)
		return
	}

	msg := &model.Message{
		ConversationID: conv.ID,
		SenderID:       admin.ID,
		ClientMsgID:    fmt.Sprintf("ann-%d-%d", time.Now().UnixNano(), admin.ID),
		MsgType:        "text",
		Content:        content,
	}
	if err := h.msgs.Create(msg); err != nil {
		internal(c, err)
		return
	}

	msgID := uint64(msg.ID)
	inbox := make([]model.UserConversation, 0, len(userIDs))
	for _, uid := range userIDs {
		inbox = append(inbox, model.UserConversation{
			UserID:         uid,
			ConversationID: conv.ID,
			LastMessageID:  &msgID,
			LastMessageAt:  &msg.CreatedAt,
		})
	}
	if err := h.msgs.UpsertInbox(inbox); err != nil {
		internal(c, err)
		return
	}
	// 未读 +1（发送者本人除外）
	if err := h.msgs.BumpUnread(conv.ID, admin.ID, msgID); err != nil {
		slog.Warn("announce unread bump", "err", err.Error())
	}

	frame := gin.H{
		"type":      "message",
		"id":        msg.ID,
		"convId":    conv.ID,
		"from":      gin.H{"id": admin.ID, "nickname": admin.Nickname},
		"content":   content,
		"msgType":   "text",
		"createdAt": msg.CreatedAt,
	}
	notifyFor(c).ToUsers(userIDs, frame)
	notifyFor(c).ToUsers(userIDs, gin.H{"type": "conv_change", "convId": conv.ID})

	h.audit(c, "announce", "system", conv.ID, "系统通知", content)
	slog.Warn("announce sent",
		"conv", conv.ID, "msg", msg.ID, "recipients", len(userIDs), "by", admin.ID)
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"convId":     conv.ID,
		"messageId":  msg.ID,
		"recipients": len(userIDs),
	})
}

// Audits 操作审计列表（最新在前）。
func (h *AdminHandler) Audits(c *gin.Context) {
	page := int(parseUint64Default(c.Query("page"), 1))
	pageSize := int(parseUint64Default(c.Query("pageSize"), 20))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	rows, total, err := h.audits.List(page, pageSize)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"audits":   rows,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}
