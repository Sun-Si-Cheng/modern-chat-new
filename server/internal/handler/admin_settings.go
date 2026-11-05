package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
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
//  3. 对象存储（S3 兼容）在启动时按库内配置构造，保存后需重启才切换存储实现；
//     图片 / 语音上传会随新配置写入对象存储。

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
	// PublicBase 对外访问前缀（CDN 域名），留空按 endpoint/bucket 拼
	PublicBase string `json:"publicBase"`
}

// siteSetting 站点信息：改了即时生效（前端拉 /api/site 拿）。
type siteSetting struct {
	Name          string `json:"name"`
	AllowRegister bool   `json:"allowRegister"`
}

// chatSetting 聊天规则：服务端每次请求现读，无需重启。
type chatSetting struct {
	MaxMessageLength int `json:"maxMessageLength"`
	RecallMinutes    int `json:"recallMinutes"`
	MaxGroupMembers  int `json:"maxGroupMembers"`
}

// uploadSetting 上传体积上限（MB）。
type uploadSetting struct {
	MaxImageMB int `json:"maxImageMB"`
	MaxAudioMB int `json:"maxAudioMB"`
	MaxFileMB  int `json:"maxFileMB"`
}

type settingsPayload struct {
	SMTP    smtpSetting    `json:"smtp"`
	Redis   redisSetting   `json:"redis"`
	Storage storageSetting `json:"storage"`
	Site    siteSetting    `json:"site"`
	Chat    chatSetting    `json:"chat"`
	Upload  uploadSetting  `json:"upload"`
}

type settingsView struct {
	SMTP    smtpSetting    `json:"smtp"`
	Redis   redisSetting   `json:"redis"`
	Storage storageSetting `json:"storage"`
	Site    siteSetting    `json:"site"`
	Chat    chatSetting    `json:"chat"`
	Upload  uploadSetting  `json:"upload"`
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
			PublicBase:   all[store.KeyStoragePublicBase],
		},
		SMTPEnabled:       h.mailer.Config().Enabled(),
		RedisEnabled:      all[store.KeyRedisAddr] != "",
		StorageApplied:    all[store.KeyStorageEndpoint] != "" && all[store.KeyStorageBucket] != "",
		RedisNeedsRestart: true,
	}

	// 站点 / 聊天 / 上传：走 Ops() 拿到的是「填过就是填的值，没填就是默认值」，
	// 所以表单永远显示当前真实生效的数值，而不是空。
	ops := h.settings.Ops()
	view.Site = siteSetting{Name: ops.SiteName, AllowRegister: ops.AllowRegister}
	view.Chat = chatSetting{
		MaxMessageLength: ops.MaxMessageLength,
		RecallMinutes:    ops.RecallMinutes,
		MaxGroupMembers:  ops.MaxGroupMembers,
	}
	view.Upload = uploadSetting{
		MaxImageMB: ops.MaxImageMB,
		MaxAudioMB: ops.MaxAudioMB,
		MaxFileMB:  ops.MaxFileMB,
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

	// 站点 / 聊天 / 上传：越界直接拒，而不是静默收敛——
	// 站长填了 50000 却被悄悄改成 5000，会以为没保存成功。
	siteName := strings.TrimSpace(req.Site.Name)
	if n := len([]rune(siteName)); n > 40 {
		fail(c, http.StatusBadRequest, "站点名称最多 40 个字符")
		return
	}
	checks := []struct {
		label string
		v     int
		min   int
		max   int
	}{
		{"单条消息长度", req.Chat.MaxMessageLength, 100, 5000},
		{"撤回时限（分钟）", req.Chat.RecallMinutes, 0, 60},
		{"群成员上限", req.Chat.MaxGroupMembers, 2, 2000},
		{"图片大小上限（MB）", req.Upload.MaxImageMB, 1, 32},
		{"语音大小上限（MB）", req.Upload.MaxAudioMB, 1, 32},
		{"群文件大小上限（MB）", req.Upload.MaxFileMB, 1, 200},
	}
	for _, ck := range checks {
		if ck.v < ck.min || ck.v > ck.max {
			fail(c, http.StatusBadRequest,
				fmt.Sprintf("%s需为 %d-%d", ck.label, ck.min, ck.max))
			return
		}
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

		store.KeyStorageEndpoint:   strings.TrimSpace(req.Storage.Endpoint),
		store.KeyStorageRegion:     strings.TrimSpace(req.Storage.Region),
		store.KeyStorageBucket:     strings.TrimSpace(req.Storage.Bucket),
		store.KeyStorageAccessKey:  strings.TrimSpace(req.Storage.AccessKey),
		store.KeyStorageSecretKey:  req.Storage.SecretKey,
		store.KeyStoragePublicBase: strings.TrimSpace(req.Storage.PublicBase),

		store.KeySiteName:          siteName,
		store.KeySiteAllowRegister: strconv.FormatBool(req.Site.AllowRegister),

		store.KeyChatMaxMessageLen:   strconv.Itoa(req.Chat.MaxMessageLength),
		store.KeyChatRecallMinutes:   strconv.Itoa(req.Chat.RecallMinutes),
		store.KeyChatMaxGroupMembers: strconv.Itoa(req.Chat.MaxGroupMembers),

		store.KeyUploadMaxImageMB: strconv.Itoa(req.Upload.MaxImageMB),
		store.KeyUploadMaxAudioMB: strconv.Itoa(req.Upload.MaxAudioMB),
		store.KeyUploadMaxFileMB:  strconv.Itoa(req.Upload.MaxFileMB),
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
		// 之前这里写死 false，与 GET 的 storageApplied 自相矛盾（保存后前端会把标签改回「未生效」）
		"storageApplied": strings.TrimSpace(req.Storage.Endpoint) != "" &&
			strings.TrimSpace(req.Storage.Bucket) != "",
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

// reloadSMTP 把库里的 SMTP 配置套到 Mailer；库里没配过（含被站长清空）时回落到环境变量。
// 注意不能直接 return：否则清空 SMTP 后 Mailer 仍保留旧配置，界面显示"已生效"却还在发信。
func (h *AdminHandler) reloadSMTP() {
	// 日志脱敏策略沿用启动配置（环境变量）
	cfg, ok := h.settings.SMTPConfig(h.cfg.SMTP.LogResetLink)
	if !ok {
		cfg = h.cfg.SMTP
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
