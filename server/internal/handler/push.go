package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// PushHandler Web Push 订阅管理。
type PushHandler struct {
	push *service.PushService
	subs *store.PushStore
}

func NewPushHandler(push *service.PushService, subs *store.PushStore) *PushHandler {
	return &PushHandler{push: push, subs: subs}
}

// Key 下发是否启用与 VAPID 公钥。
func (h *PushHandler) Key(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"enabled": h.push != nil && h.push.Enabled(),
		"key":     h.push.PublicKey(),
	})
}

// Subscribe 保存浏览器推送订阅。
func (h *PushHandler) Subscribe(c *gin.Context) {
	var req struct {
		Endpoint string `json:"endpoint"`
		P256dh   string `json:"p256dh"`
		Auth     string `json:"auth"`
	}
	if !bind(c, &req) {
		return
	}
	endpoint := strings.TrimSpace(req.Endpoint)
	p256dh := strings.TrimSpace(req.P256dh)
	auth := strings.TrimSpace(req.Auth)
	if !strings.HasPrefix(endpoint, "https://") || len(endpoint) > 500 ||
		p256dh == "" || len(p256dh) > 255 || auth == "" || len(auth) > 128 {
		fail(c, http.StatusBadRequest, "推送订阅参数不合法")
		return
	}
	if err := h.subs.Upsert(&model.PushSubscription{
		UserID: middleware.UserFrom(c).ID, Endpoint: endpoint, P256dh: p256dh, Auth: auth,
	}); err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Unsubscribe 删除订阅（用户关闭推送）。endpoint 可放查询串或 JSON body。
func (h *PushHandler) Unsubscribe(c *gin.Context) {
	endpoint := strings.TrimSpace(c.Query("endpoint"))
	if endpoint == "" {
		var req struct {
			Endpoint string `json:"endpoint"`
		}
		if !bind(c, &req) {
			return
		}
		endpoint = strings.TrimSpace(req.Endpoint)
	}
	if endpoint == "" {
		fail(c, http.StatusBadRequest, "endpoint 不能为空")
		return
	}
	if err := h.subs.Remove(endpoint); err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
