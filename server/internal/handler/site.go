package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// SiteHandler 公开的站点信息（无需登录）。
//
// 前端用它做三件事：显示站点名、决定要不要露出注册入口、
// 以及把消息长度 / 撤回时限 / 上传体积这些规则同步到输入区。
// 只暴露「能公开」的字段——SMTP、Redis、对象存储密钥一律不在这里。
type SiteHandler struct {
	settings *store.SettingStore
}

func NewSiteHandler(settings *store.SettingStore) *SiteHandler {
	return &SiteHandler{settings: settings}
}

// Get GET /api/site
func (h *SiteHandler) Get(c *gin.Context) {
	ops := store.DefaultOps()
	if h.settings != nil {
		ops = h.settings.Ops()
	}
	c.JSON(http.StatusOK, gin.H{
		"name":             ops.SiteName,
		"allowRegister":    ops.AllowRegister,
		"maxMessageLength": ops.MaxMessageLength,
		"recallMinutes":    ops.RecallMinutes,
		"maxGroupMembers":  ops.MaxGroupMembers,
		"maxImageMB":       ops.MaxImageMB,
		"maxAudioMB":       ops.MaxAudioMB,
		"maxFileMB":        ops.MaxFileMB,
	})
}
