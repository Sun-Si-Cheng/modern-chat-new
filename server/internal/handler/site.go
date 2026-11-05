package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// SiteHandler 公开的站点信息（无需登录）。
//
// 前端用它做三件事：显示站点名、决定要不要露出注册入口、
// 以及把消息长度 / 撤回时限 / 上传体积这些规则同步到输入区。
// 只暴露「能公开」的字段——SMTP、Redis、对象存储密钥一律不在这里。
type SiteHandler struct {
	settings *store.SettingStore
	mailer   *service.Mailer
	users    *store.UserStore
}

func NewSiteHandler(settings *store.SettingStore, mailer *service.Mailer, users *store.UserStore) *SiteHandler {
	return &SiteHandler{settings: settings, mailer: mailer, users: users}
}

// Get GET /api/site
func (h *SiteHandler) Get(c *gin.Context) {
	ops := store.DefaultOps()
	if h.settings != nil {
		ops = h.settings.Ops()
	}
	// 邮箱相关报「实际生效值」，前端据此决定渲染哪些字段：
	//   requireEmail       —— 注册是否必填邮箱（开关开启即必填，首个账号也是）
	//   requireEmailVerify —— 是否还需要验证码（开关 && SMTP 可用 && 已有账号；
	//                         首个账号豁免、未配 SMTP 也不强制，否则会把部署者锁在门外）
	mailerReady := h.mailer != nil && h.mailer.Config().Enabled()
	userCount := int64(0)
	if h.users != nil {
		if n, err := h.users.Count(); err == nil {
			userCount = n
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"name":               ops.SiteName,
		"allowRegister":      ops.AllowRegister,
		"maintenanceMode":    ops.MaintenanceMode,
		"maintenanceMessage": ops.MaintenanceMessage,
		"requireInviteCode":  ops.RequireInviteCode,
		"requireEmail":       ops.RequireEmailVerify,
		"requireEmailVerify": ops.RequireEmailVerify && mailerReady && userCount > 0,
		"maxMessageLength":   ops.MaxMessageLength,
		"recallMinutes":      ops.RecallMinutes,
		"maxGroupMembers":    ops.MaxGroupMembers,
		"maxImageMB":         ops.MaxImageMB,
		"maxAudioMB":         ops.MaxAudioMB,
		"maxFileMB":          ops.MaxFileMB,
		"maxVideoMB":         ops.MaxVideoMB,
	})
}
