package service

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// ErrPushGone 端点已失效（浏览器卸载/清空站点数据），调用方应删除该订阅。
var ErrPushGone = errors.New("push subscription gone")

const (
	// pushTimeout 单次推送超时：webpush-go 默认用无超时的 http.Client，
	// 端点不响应时会永久占用 goroutine 与连接。
	pushTimeout = 10 * time.Second
	// pushMaxConcurrent 同时向浏览器推送服务的出站请求上限。
	// 大群群发时避免瞬间打出成百上千条外连、耗尽本机连接/文件描述符。
	pushMaxConcurrent = 16
)

// pushEndpointHosts 允许的 Web Push 服务域名。
// endpoint 由客户端提交，服务端会向它发起 POST；仅允许主流浏览器的推送网关，
// 避免被填成内网地址或任意域名（SSRF / 被当成探测代理）。
var pushEndpointHosts = []string{
	"fcm.googleapis.com",
	"android.googleapis.com",
	"push.services.mozilla.com",
	"notify.windows.com",
	"push.apple.com",
	"webpush.apple.com",
}

// ValidPushEndpoint 校验推送端点：必须 https、无 userinfo、域名属于已知推送网关。
func ValidPushEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range pushEndpointHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// PushService Web Push（VAPID）。未配置密钥时 Enabled=false，所有推送静默跳过。
type PushService struct {
	publicKey  string
	privateKey string
	subject    string
	client     *http.Client
	sem        chan struct{}
}

func NewPushService(publicKey, privateKey, subject string) *PushService {
	if subject == "" {
		subject = "mailto:admin@localhost"
	}
	return &PushService{
		publicKey:  publicKey,
		privateKey: privateKey,
		subject:    subject,
		client:     &http.Client{Timeout: pushTimeout},
		sem:        make(chan struct{}, pushMaxConcurrent),
	}
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
	// 并发上限：满了就丢弃本次推送（消息已在库里，用户下次打开仍可见），
	// 不排队堆积 goroutine。
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	default:
		slog.Warn("web push concurrency limit reached, drop")
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
		HTTPClient:      p.client,
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
