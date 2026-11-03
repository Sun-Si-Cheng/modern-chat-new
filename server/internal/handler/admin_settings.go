package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// ===== 系统设置（站长可改，落库覆盖环境变量）=====
//
// 设计要点：
//  1. 敏感字段（SMTP 密码 / Redis 密码 / 对象存储 SecretKey）**从不回显**，
//     只返回 hasXxx 表示"是否已设置"；提交时留空表示不修改。
//  2. SMTP 保存后立即热生效（Mailer 支持热更新）；Redis 配置在启动时构造限流器，
//     改完必须重启，接口会显式提示。
//  3. 对象存储目前**没有消费方**（项目还没有文件上传，头像只接受 URL 字符串），
//     保存后不会生效，接口用 storageApplied=false 明确告知。

type smtpSetting struct {
	Host string `json:"host"`
	Port string `json:"port"`
	User string `json:"user"`
	From string `json:"from"`
	// Pass 仅用于提交；读取时为空 → omitempty 不出现在响应里
	Pass    string `json:"pass,omitempty"`
	HasPass bool   `json:"hasPass"`
}

type redisSetting struct {
	Addr string `json:"addr"`
	DB   string `json:"db"`
	Pass string `json:"pass,omitempty"`
	// HasPass 回显是否已设置密码
	HasPass bool `json:"hasPass"`
}

type storageSetting struct {
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region"`
	Bucket       string `json:"bucket"`
	AccessKey    string `json:"accessKey"`
	SecretKey    string `json:"secretKey,omitempty"`
	HasSecretKey bool   `json:"hasSecretKey"`
}

type settingsPayload struct {
	SMTP    smtpSetting    `json:"smtp"`
	Redis   redisSetting   `json:"redis"`
	Storage storageSetting `json:"storage"`
}

type settingsView struct {
	SMTP    smtpSetting    `json:"smtp"`
	Redis   redisSetting   `json:"redis"`
	Storage storageSetting `json:"storage"`
	// 生效状态
	SMTPEnabled    bool `json:"smtpEnabled"`
	RedisEnabled   bool `json:"redisEnabled"`
	StorageApplied bool `json:"storageApplied"`
	// Redis 限流器在启动时构造，改动需重启
	RedisNeedsRestart bool `json:"redisNeedsRestart"`
}

// GetSettings 读取系统设置（敏感项只给 hasXxx）。
func (h *AdminHandler) GetSettings(c *gin.Context) {
	all, err := h.settings.All()
	if err != nil {
		internal(c, err)
		return
	}
	view := settingsView{
		SMTP: smtpSetting{
			Host:    all[store.KeySMTPHost],
			Port:    all[store.KeySMTPPort],
			User:    all[store.KeySMTPUser],
			From:    all[store.KeySMTPFrom],
			HasPass: all[store.KeySMTPPass] != "",
		},
		Redis: redisSetting{
			Addr:    all[store.KeyRedisAddr],
			DB:      all[store.KeyRedisDB],
			HasPass: all[store.KeyRedisPass] != "",
		},
		Storage: storageSetting{
			Endpoint:     all[store.KeyStorageEndpoint],
			Region:       all[store.KeyStorageRegion],
			Bucket:       all[store.KeyStorageBucket],
			AccessKey:    all[store.KeyStorageAccessKey],
			HasSecretKey: all[store.KeyStorageSecretKey] != "",
		},
		SMTPEnabled:       h.mailer.Config().Enabled(),
		RedisEnabled:      all[store.KeyRedisAddr] != "",
		StorageApplied:    false,
		RedisNeedsRestart: true,
	}
	c.JSON(http.StatusOK, view)
}

// SaveSettings 保存系统设置并让 SMTP 立即生效。
func (h *AdminHandler) SaveSettings(c *gin.Context) {
	var req settingsPayload
	if !bind(c, &req) {
		return
	}

	// 校验：端口必须数字；发件人邮箱格式（同时防 SMTP 头注入，见 validateEmail）
	if p := strings.TrimSpace(req.SMTP.Port); p != "" && !isAllDigits(p) {
		fail(c, http.StatusBadRequest, "SMTP 端口必须是数字")
		return
	}
	if msg := validateEmail(strings.TrimSpace(req.SMTP.From)); msg != "" {
		fail(c, http.StatusBadRequest, "发件人"+msg)
		return
	}
	if db := strings.TrimSpace(req.Redis.DB); db != "" && !isAllDigits(db) {
		fail(c, http.StatusBadRequest, "Redis DB 必须是数字")
		return
	}

	updates := map[string]string{
		store.KeySMTPHost: strings.TrimSpace(req.SMTP.Host),
		store.KeySMTPPort: strings.TrimSpace(req.SMTP.Port),
		store.KeySMTPUser: strings.TrimSpace(req.SMTP.User),
		store.KeySMTPFrom: strings.TrimSpace(req.SMTP.From),
		store.KeySMTPPass: req.SMTP.Pass,

		store.KeyRedisAddr: strings.TrimSpace(req.Redis.Addr),
		store.KeyRedisDB:   strings.TrimSpace(req.Redis.DB),
		store.KeyRedisPass: req.Redis.Pass,

		store.KeyStorageEndpoint:  strings.TrimSpace(req.Storage.Endpoint),
		store.KeyStorageRegion:    strings.TrimSpace(req.Storage.Region),
		store.KeyStorageBucket:    strings.TrimSpace(req.Storage.Bucket),
		store.KeyStorageAccessKey: strings.TrimSpace(req.Storage.AccessKey),
		store.KeyStorageSecretKey: req.Storage.SecretKey,
	}

	if err := h.settings.Save(updates, middleware.UserFrom(c).ID); err != nil {
		internal(c, err)
		return
	}

	// SMTP 热生效
	h.reloadSMTP()

	h.audit(c, "save_settings", "system", 0, "系统设置", describeSettings(req))
	slog.Warn("system settings saved", "by", middleware.UserFrom(c).ID)

	c.JSON(http.StatusOK, gin.H{
		"ok":                true,
		"redisNeedsRestart": strings.TrimSpace(req.Redis.Addr) != "",
		"storageApplied":    false,
	})
}

// TestSMTP 用当前配置发一封测试邮件，返回具体失败原因（配错了要看得到）。
func (h *AdminHandler) TestSMTP(c *gin.Context) {
	var req struct {
		To string `json:"to"`
	}
	if !bind(c, &req) {
		return
	}
	to := strings.TrimSpace(req.To)
	if msg := validateEmail(to); msg != "" {
		fail(c, http.StatusBadRequest, "收件"+msg)
		return
	}
	if to == "" {
		fail(c, http.StatusBadRequest, "请填写收件邮箱")
		return
	}

	h.reloadSMTP() // 确保用最新配置
	if err := h.mailer.SendTest(to); err != nil {
		slog.Warn("smtp test failed", "to", to, "err", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "发送失败：" + err.Error()})
		return
	}
	h.audit(c, "test_smtp", "system", 0, "SMTP", "to="+to)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// reloadSMTP 把库里的 SMTP 配置套到 Mailer；库里没配过则保留环境变量配置。
func (h *AdminHandler) reloadSMTP() {
	// 日志脱敏策略沿用启动配置（环境变量）
	cfg, ok := h.settings.SMTPConfig(h.cfg.SMTP.LogResetLink)
	if !ok {
		return
	}
	h.mailer.SetConfig(cfg)
}

func describeSettings(req settingsPayload) string {
	parts := make([]string, 0, 3)
	if req.SMTP.Host != "" {
		parts = append(parts, "smtp="+req.SMTP.Host)
	}
	if req.Redis.Addr != "" {
		parts = append(parts, "redis="+req.Redis.Addr)
	}
	if req.Storage.Endpoint != "" {
		parts = append(parts, "storage="+req.Storage.Endpoint)
	}
	return strings.Join(parts, " ")
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
