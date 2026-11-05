package service

import (
	"encoding/json"
	"errors"
	"log/slog"

	webpush "github.com/SherClockHolmes/webpush-go"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// ErrPushGone 端点已失效（浏览器卸载/清空站点数据），调用方应删除该订阅。
var ErrPushGone = errors.New("push subscription gone")

// PushService Web Push（VAPID）。未配置密钥时 Enabled=false，所有推送静默跳过。
type PushService struct {
	publicKey  string
	privateKey string
	subject    string
}

func NewPushService(publicKey, privateKey, subject string) *PushService {
	if subject == "" {
		subject = "mailto:admin@localhost"
	}
	return &PushService{publicKey: publicKey, privateKey: privateKey, subject: subject}
}

func (p *PushService) Enabled() bool { return p != nil && p.publicKey != "" && p.privateKey != "" }

// PublicKey 下发给前端用于 pushManager.subscribe。
func (p *PushService) PublicKey() string {
	if p == nil {
		return ""
	}
	return p.publicKey
}

// Send 推送一条 JSON 消息；410/404 视为端点失效。
func (p *PushService) Send(sub *model.PushSubscription, payload any) error {
	if !p.Enabled() {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := webpush.SendNotification(body, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		Subscriber:      p.subject,
		VAPIDPublicKey:  p.publicKey,
		VAPIDPrivateKey: p.privateKey,
		TTL:             3600,
	})
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode == 404 || resp.StatusCode == 410 {
		return ErrPushGone
	}
	if resp.StatusCode >= 300 {
		slog.Warn("web push rejected", "status", resp.StatusCode, "endpoint", sub.Endpoint)
	}
	return nil
}
