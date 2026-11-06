package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// AdminUpdateHandler 站长后台的图形化更新（检查 / 启动 / 状态轮询）。
// 仅站长可用；命令固定参数无注入面；部署方式不支持时接口返回 400 + 原因。
type AdminUpdateHandler struct {
	updater *service.Updater
	audits  *store.AuditStore
}

func NewAdminUpdateHandler(updater *service.Updater, audits *store.AuditStore) *AdminUpdateHandler {
	return &AdminUpdateHandler{updater: updater, audits: audits}
}

// audit 记录更新操作（写失败不影响主流程）。
func (h *AdminUpdateHandler) audit(c *gin.Context, action, detail string) {
	admin := middleware.UserFrom(c)
	if admin == nil || h.audits == nil {
		return
	}
	if err := h.audits.Record(store.AuditEntry{
		AdminID:    admin.ID,
		Action:     action,
		TargetType: "system",
		TargetName: "web_update",
		Detail:     detail,
		IP:         c.ClientIP(),
	}); err != nil {
		slog.Warn("admin audit write failed", "action", action, "err", err.Error())
	}
}

// ensureAvailable 部署方式不支持时统一 400。
func (h *AdminUpdateHandler) ensureAvailable(c *gin.Context) bool {
	if h.updater == nil || !h.updater.Available() {
		fail(c, http.StatusBadRequest, "当前部署方式不支持在线更新（需要 scripts/install.sh 的 systemd 部署；Docker / 裸机请用 scripts/update.sh）")
		return false
	}
	return true
}

// Status 更新状态与最近日志（前端 2s 轮询）。
func (h *AdminUpdateHandler) Status(c *gin.Context) {
	if h.updater == nil {
		c.JSON(http.StatusOK, service.StatusView{Available: false})
		return
	}
	c.JSON(http.StatusOK, h.updater.Status())
}

// Check 检查远端是否有新版本。
func (h *AdminUpdateHandler) Check(c *gin.Context) {
	if !h.ensureAvailable(c) {
		return
	}
	res, err := h.updater.Check()
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}

// Start 启动更新任务（异步执行；前端轮询 Status 看进度）。
func (h *AdminUpdateHandler) Start(c *gin.Context) {
	if !h.ensureAvailable(c) {
		return
	}
	if !h.updater.Start() {
		fail(c, http.StatusConflict, "有更新任务正在进行中，请稍候")
		return
	}
	h.audit(c, "web_update", "站长触发了在线更新（拉取最新代码并重启服务）")
	c.JSON(http.StatusOK, gin.H{"started": true})
}
