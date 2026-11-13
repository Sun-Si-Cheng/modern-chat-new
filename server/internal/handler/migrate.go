package handler

// 旧站迁移 HTTP 接口（契约：modern-chat-new/docs/旧站迁移方案.md §4）。
//
// 所有接口都是 JSON POST；除 /api/auth/migrate-login（在 auth.go）外，
// 必须携带请求头 X-Migrate-Secret，与 LEGACY_API_SECRET 做常量时间比较。
// 业务逻辑全部在 service.MigrateService，本文件只做参数校验与错误到
// HTTP 状态码的映射（契约 §4 的错误语义：503 未启用、401 密钥/票据错误、
// 404 用户或任务不存在、409 已迁移/未完成）。

import (
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
)

// MigrateHandler 旧站迁移接口。
type MigrateHandler struct {
	cfg config.Config
	svc *service.MigrateService
}

// NewMigrateHandler 构造迁移接口处理器；svc 在迁移功能关闭时也不会连接旧库。
func NewMigrateHandler(cfg config.Config, svc *service.MigrateService) *MigrateHandler {
	return &MigrateHandler{cfg: cfg, svc: svc}
}

// guard 统一校验「功能已启用 + X-Migrate-Secret 正确」。
// 返回 false 时响应已写好，调用方直接 return。
func (h *MigrateHandler) guard(c *gin.Context) bool {
	if h.svc == nil || !h.svc.Enabled() {
		fail(c, http.StatusServiceUnavailable, "迁移功能未启用")
		return false
	}
	got := c.GetHeader("X-Migrate-Secret")
	if subtle.ConstantTimeCompare([]byte(got), []byte(h.cfg.LegacyAPISecret)) != 1 {
		fail(c, http.StatusUnauthorized, "无效的迁移密钥")
		return false
	}
	return true
}

// failMigrate 把 service 层错误映射成契约约定的 HTTP 状态码。
func (h *MigrateHandler) failMigrate(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrMigrateDisabled):
		fail(c, http.StatusServiceUnavailable, "迁移功能未启用")
	case errors.Is(err, service.ErrLegacyUnavailable):
		fail(c, http.StatusServiceUnavailable, "旧站数据库不可用，请稍后再试")
	case errors.Is(err, service.ErrLegacyUserNotFound):
		fail(c, http.StatusNotFound, "旧站用户不存在")
	case errors.Is(err, service.ErrAlreadyMigrated):
		fail(c, http.StatusConflict, "已迁移")
	case errors.Is(err, service.ErrJobNotFound):
		fail(c, http.StatusNotFound, "迁移任务不存在")
	case errors.Is(err, service.ErrJobNotFinished):
		fail(c, http.StatusConflict, "迁移尚未完成")
	case errors.Is(err, service.ErrNoLegacyMap):
		fail(c, http.StatusNotFound, "该用户尚未迁移")
	case errors.Is(err, service.ErrTicketInvalid):
		fail(c, http.StatusUnauthorized, "登录票据无效或已过期")
	default:
		internal(c, err)
	}
}

// Preview 预览旧站用户与可迁移数据量（§4.1，只读）。
// POST /api/migrate/preview  {"legacyUserId": 123}
func (h *MigrateHandler) Preview(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	var req struct {
		LegacyUserID int32 `json:"legacyUserId"`
	}
	if !bind(c, &req) {
		return
	}
	if req.LegacyUserID <= 0 {
		fail(c, http.StatusBadRequest, "legacyUserId 无效")
		return
	}
	res, err := h.svc.Preview(req.LegacyUserID)
	if err != nil {
		h.failMigrate(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// Start 创建迁移任务（§4.2）。
// POST /api/migrate/start  {"legacyUserId": 123, "excludePrivateIds": [...], "excludeGroupIds": [...]}
// 排除列表语义：省略或为 null = 全部迁移；列表里是「明确不要迁移的旧消息 id」，
// 其余全部迁移（空数组等同不排除）。这样 preview 只展示 50 条样例也不会误删数据。
func (h *MigrateHandler) Start(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	var req struct {
		LegacyUserID      int32    `json:"legacyUserId"`
		ExcludePrivateIDs *[]int64 `json:"excludePrivateIds"`
		ExcludeGroupIDs   *[]int64 `json:"excludeGroupIds"`
	}
	if !bind(c, &req) {
		return
	}
	if req.LegacyUserID <= 0 {
		fail(c, http.StatusBadRequest, "legacyUserId 无效")
		return
	}
	res, err := h.svc.Start(req.LegacyUserID, excludeIDList(req.ExcludePrivateIDs), excludeIDList(req.ExcludeGroupIDs))
	if err != nil {
		h.failMigrate(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// Tick 推进迁移（§4.3，每次最多 200 条消息或一个阶段）。
// POST /api/migrate/tick  {"jobId": "..."}
func (h *MigrateHandler) Tick(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	var req struct {
		JobID string `json:"jobId"`
	}
	if !bind(c, &req) {
		return
	}
	if req.JobID == "" {
		fail(c, http.StatusBadRequest, "jobId 不能为空")
		return
	}
	res, err := h.svc.Tick(req.JobID)
	if err != nil {
		h.failMigrate(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// Complete 迁移完成后签发票据（§4.4）。
// POST /api/migrate/complete  {"jobId": "..."}
func (h *MigrateHandler) Complete(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	var req struct {
		JobID string `json:"jobId"`
	}
	if !bind(c, &req) {
		return
	}
	if req.JobID == "" {
		fail(c, http.StatusBadRequest, "jobId 不能为空")
		return
	}
	res, err := h.svc.Complete(req.JobID)
	if err != nil {
		h.failMigrate(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// CompleteExisting 对已迁移用户直接签发票据（§4.4 complete-existing）。
// POST /api/migrate/complete-existing  {"legacyUserId": 123}
func (h *MigrateHandler) CompleteExisting(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	var req struct {
		LegacyUserID int32 `json:"legacyUserId"`
	}
	if !bind(c, &req) {
		return
	}
	if req.LegacyUserID <= 0 {
		fail(c, http.StatusBadRequest, "legacyUserId 无效")
		return
	}
	res, err := h.svc.CompleteExisting(req.LegacyUserID)
	if err != nil {
		h.failMigrate(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// excludeIDList 把排除列表转成去重后的旧消息 id 切片：
// nil（字段省略或为 null）表示「不排除任何消息」；空数组同样返回空切片。
func excludeIDList(ids *[]int64) []int64 {
	if ids == nil {
		return nil
	}
	seen := make(map[int64]bool, len(*ids))
	out := make([]int64, 0, len(*ids))
	for _, id := range *ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
