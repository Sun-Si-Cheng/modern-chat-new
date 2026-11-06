package store

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// 已知配置键。只有这里列出的键才能通过后台写入，
// 避免变成一个任意 key 的"伪 KV 数据库"。
const (
	KeySMTPHost = "smtp.host"
	KeySMTPPort = "smtp.port"
	KeySMTPUser = "smtp.user"
	KeySMTPPass = "smtp.pass" // 加密
	KeySMTPFrom = "smtp.from"

	KeyRedisAddr = "redis.addr"
	KeyRedisPass = "redis.pass" // 加密
	KeyRedisDB   = "redis.db"

	// 对象存储：S3 兼容（MinIO / AWS S3 / OSS / COS）。Endpoint + Bucket 都配置时
	// 上传走对象存储；否则回落到本地磁盘（DATA_DIR/uploads）。
	KeyStorageEndpoint  = "storage.endpoint"
	KeyStorageRegion    = "storage.region"
	KeyStorageBucket    = "storage.bucket"
	KeyStorageAccessKey = "storage.accessKey"
	KeyStorageSecretKey = "storage.secretKey" // 加密
	// PublicBase 对象存储的对外访问前缀（CDN 域名）。留空则按 endpoint/bucket 拼。
	KeyStoragePublicBase = "storage.publicBase"

	// 站点
	KeySiteName              = "site.name"
	KeySiteAllowRegister     = "site.allowRegister"
	KeySiteMaintenanceMode   = "site.maintenanceMode"
	KeySiteMaintenanceMsg    = "site.maintenanceMessage"
	KeySiteRequireInviteCode = "site.requireInviteCode"
	// 注册需邮箱验证：默认开启；开启时注册必须带邮箱 + 邮件验证码。
	// 首个账号（站长）始终豁免，保证没有 SMTP 时也能把站点引导起来。
	KeySiteRequireEmailVerify = "site.requireEmailVerify"

	// 安全
	KeySecurityLoginMaxAttempts = "security.loginMaxAttempts"
	KeySecurityLoginLockMinutes = "security.loginLockMinutes"

	// 聊天规则
	KeyChatMaxMessageLen   = "chat.maxMessageLength"
	KeyChatRecallMinutes   = "chat.recallMinutes"
	KeyChatMaxGroupMembers = "chat.maxGroupMembers"
	// 敏感词：换行分隔；文本消息命中即拒绝发送
	KeyChatBlockedWords = "chat.blockedWords"

	// 上传体积上限（MB）
	KeyUploadMaxImageMB = "upload.maxImageMB"
	KeyUploadMaxAudioMB = "upload.maxAudioMB"
	KeyUploadMaxFileMB  = "upload.maxFileMB"
	// 视频（MB）：手机短视频体积大，默认给 30
	KeyUploadMaxVideoMB = "upload.maxVideoMB"
	// 本地上传目录总配额（MB，0=不限制）
	KeyUploadMaxStorageMB = "upload.maxStorageMB"
)

var knownKeys = map[string]bool{
	KeySMTPHost: true, KeySMTPPort: true, KeySMTPUser: true, KeySMTPPass: true, KeySMTPFrom: true,
	KeyRedisAddr: true, KeyRedisPass: true, KeyRedisDB: true,
	KeyStorageEndpoint: true, KeyStorageRegion: true, KeyStorageBucket: true,
	KeyStorageAccessKey: true, KeyStorageSecretKey: true, KeyStoragePublicBase: true,

	KeySiteName: true, KeySiteAllowRegister: true,
	KeySiteMaintenanceMode: true, KeySiteMaintenanceMsg: true, KeySiteRequireInviteCode: true,
	KeySiteRequireEmailVerify: true,
	KeyChatMaxMessageLen:      true, KeyChatRecallMinutes: true, KeyChatMaxGroupMembers: true,
	KeyChatBlockedWords: true,
	KeyUploadMaxImageMB: true, KeyUploadMaxAudioMB: true, KeyUploadMaxFileMB: true,
	KeyUploadMaxVideoMB:         true,
	KeyUploadMaxStorageMB:       true,
	KeySecurityLoginMaxAttempts: true, KeySecurityLoginLockMinutes: true,
}

// secretKeys 落库前加密、返回前端时脱敏。
var secretKeys = map[string]bool{
	KeySMTPPass:         true,
	KeyRedisPass:        true,
	KeyStorageSecretKey: true,
}

// IsSecretKey 判断是否敏感字段。
func IsSecretKey(key string) bool { return secretKeys[key] }

// IsKnownKey 判断是否允许后台写入。
func IsKnownKey(key string) bool { return knownKeys[key] }

// SettingStore 系统配置的读写。带进程内缓存，避免每次读配置都查库。
type SettingStore struct {
	db  *gorm.DB
	box *SecretBox

	mu    sync.RWMutex
	cache map[string]string // 解密后的明文缓存
	// ops 解析后的业务规则缓存。
	//
	// Ops() 在每个请求里至少被调用 2 次（维护中间件 + handler 本身），
	// 每次都从 map 深拷贝再解析 ~20 个键（TrimSpace / strconv / 敏感词拆分）。
	// 配置很少变，这里缓存解析结果；Save/Invalidate 时置空重算。
	ops atomic.Pointer[Ops]
}

func NewSettingStore(db *gorm.DB, box *SecretBox) *SettingStore {
	return &SettingStore{db: db, box: box}
}

func (s *SettingStore) load() (map[string]string, error) {
	var rows []model.SystemSetting
	if err := s.db.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.Encrypted {
			plain, err := s.box.Open(r.Value)
			if err != nil {
				// 单个值解不开（多半是换过 JWT_SECRET）不应拖垮整站：
				// 跳过该键，让调用方回落到环境变量。
				continue
			}
			out[r.Key] = plain
			continue
		}
		out[r.Key] = r.Value
	}
	return out, nil
}

// All 返回解密后的全部配置（内部使用，不回前端）。
func (s *SettingStore) All() (map[string]string, error) {
	s.mu.RLock()
	if s.cache != nil {
		cp := make(map[string]string, len(s.cache))
		for k, v := range s.cache {
			cp[k] = v
		}
		s.mu.RUnlock()
		return cp, nil
	}
	s.mu.RUnlock()

	loaded, err := s.load()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache = loaded
	s.mu.Unlock()

	cp := make(map[string]string, len(loaded))
	for k, v := range loaded {
		cp[k] = v
	}
	return cp, nil
}

// Get 读单个配置，缺失返回空串。
func (s *SettingStore) Get(key string) string {
	all, err := s.All()
	if err != nil {
		return ""
	}
	return all[key]
}

// Save 写入配置。空字符串的敏感字段表示「不修改」，
// 避免前端不回显密码时把已存的密码清空。
func (s *SettingStore) Save(updates map[string]string, by uint) error {
	for key, val := range updates {
		if !IsKnownKey(key) {
			continue
		}
		if IsSecretKey(key) && val == "" {
			continue
		}
		stored := val
		if IsSecretKey(key) {
			enc, err := s.box.Seal(val)
			if err != nil {
				return err
			}
			stored = enc
		}
		row := model.SystemSetting{
			Key:       key,
			Value:     stored,
			Encrypted: IsSecretKey(key),
			UpdatedBy: by,
		}
		// upsert：按主键覆盖
		if err := s.db.Save(&row).Error; err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.cache = nil // 失效缓存，下次读取重新加载
	s.mu.Unlock()
	s.ops.Store(nil) // 解析结果一并失效，否则新配置要等进程重启才生效
	return nil
}

// Invalidate 丢弃缓存（例如外部直接改库后）。
func (s *SettingStore) Invalidate() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
	s.ops.Store(nil)
}

// Ops 可在后台调整的业务规则（站点 / 聊天 / 上传）。
//
// 与 SMTP/Redis/存储这类「改了要重启」的配置不同，这些是**即时生效**的：
// 读的是 SettingStore 的缓存，Save 时缓存失效，下一次请求就是新值。
type Ops struct {
	SiteName           string
	AllowRegister      bool
	MaintenanceMode    bool
	MaintenanceMessage string
	RequireInviteCode  bool
	// RequireEmailVerify 注册需邮箱验证（默认开）
	RequireEmailVerify bool
	MaxMessageLength   int
	RecallMinutes      int
	MaxGroupMembers    int
	// BlockedWords 敏感词（已按行拆分、去空、去首尾空白）
	BlockedWords []string
	MaxImageMB   int
	MaxAudioMB   int
	MaxFileMB    int
	MaxVideoMB   int
	// MaxStorageMB 本地上传目录总配额（0=不限制）
	MaxStorageMB     int
	LoginMaxAttempts int
	LoginLockMinutes int
}

// DefaultOps 缺省值。后台没配过任何一项时的行为，与改造前完全一致。
func DefaultOps() Ops {
	return Ops{
		SiteName:           "ModernChat",
		AllowRegister:      true,
		MaintenanceMode:    false,
		MaintenanceMessage: "站点正在维护，请稍后再试。",
		RequireInviteCode:  false,
		// 默认要求邮箱验证：站点的第一件事通常是配好 SMTP。
		// 首个账号（站长）在 Register 里豁免，不会把部署者锁在门外。
		RequireEmailVerify: true,
		MaxMessageLength:   1000,
		RecallMinutes:      2,
		MaxGroupMembers:    200,
		MaxImageMB:         8,
		MaxAudioMB:         5,
		MaxFileMB:          20,
		MaxVideoMB:         30,
		LoginMaxAttempts:   5,
		LoginLockMinutes:   15,
	}
}

// Ops 读取业务规则。任何一项缺失或非法都回落到默认值，
// 不会因为后台存了个错值就把功能卡死。
//
// 返回值是结构体拷贝（与之前语义一致）；内部切片字段（敏感词）只读，
// 调用方不要原地修改。
func (s *SettingStore) Ops() Ops {
	if p := s.ops.Load(); p != nil {
		return *p
	}
	all, err := s.All()
	var out Ops
	if err != nil {
		out = DefaultOps()
	} else {
		out = OpsFrom(all)
	}
	s.ops.Store(&out)
	return out
}

// OpsFrom 把原始 KV 解析成 Ops（纯函数，便于单测）。
func OpsFrom(all map[string]string) Ops {
	def := DefaultOps()
	out := def

	if v := strings.TrimSpace(all[KeySiteName]); v != "" {
		// 站点名过长会撑坏标题栏与登录页，按 rune 截断
		if r := []rune(v); len(r) > 40 {
			v = string(r[:40])
		}
		out.SiteName = v
	}
	// 只有显式写 "false" 才算关闭注册：没配过 / 配了脏值都保持开放，
	// 避免一个手误把所有人都挡在门外
	switch strings.TrimSpace(all[KeySiteAllowRegister]) {
	case "false":
		out.AllowRegister = false
	case "true":
		out.AllowRegister = true
	}
	if strings.TrimSpace(all[KeySiteMaintenanceMode]) == "true" {
		out.MaintenanceMode = true
	}
	if v := strings.TrimSpace(all[KeySiteMaintenanceMsg]); v != "" {
		if r := []rune(v); len(r) > 200 {
			v = string(r[:200])
		}
		out.MaintenanceMessage = v
	}
	if strings.TrimSpace(all[KeySiteRequireInviteCode]) == "true" {
		out.RequireInviteCode = true
	}
	// 默认开：只有显式写 "false" 才关闭（与 AllowRegister 的「默认开」口径相反，
	// 因为这里的安全默认值是「要验证」，而不是「不验证」）
	if strings.TrimSpace(all[KeySiteRequireEmailVerify]) == "false" {
		out.RequireEmailVerify = false
	}

	out.MaxMessageLength = clampInt(all[KeyChatMaxMessageLen], 100, 5000, def.MaxMessageLength)
	out.RecallMinutes = clampInt(all[KeyChatRecallMinutes], 0, 60, def.RecallMinutes)
	out.MaxGroupMembers = clampInt(all[KeyChatMaxGroupMembers], 2, 2000, def.MaxGroupMembers)
	out.BlockedWords = splitBlockedWords(all[KeyChatBlockedWords])
	out.MaxImageMB = clampInt(all[KeyUploadMaxImageMB], 1, 32, def.MaxImageMB)
	out.MaxAudioMB = clampInt(all[KeyUploadMaxAudioMB], 1, 32, def.MaxAudioMB)
	out.MaxFileMB = clampInt(all[KeyUploadMaxFileMB], 1, 200, def.MaxFileMB)
	out.MaxVideoMB = clampInt(all[KeyUploadMaxVideoMB], 1, 100, def.MaxVideoMB)
	// 0 = 不限制；其它值 1-102400（100 GB）
	if raw := strings.TrimSpace(all[KeyUploadMaxStorageMB]); raw == "0" || raw == "" {
		out.MaxStorageMB = 0
	} else {
		out.MaxStorageMB = clampInt(raw, 1, 102400, 0)
	}
	// 0 = 关闭账号锁定；其它值收敛到 2-20 次
	if raw := strings.TrimSpace(all[KeySecurityLoginMaxAttempts]); raw == "0" {
		out.LoginMaxAttempts = 0
	} else {
		out.LoginMaxAttempts = clampInt(raw, 2, 20, def.LoginMaxAttempts)
	}
	out.LoginLockMinutes = clampInt(all[KeySecurityLoginLockMinutes], 1, 1440, def.LoginLockMinutes)
	return out
}

// splitBlockedWords 把后台填写的多行敏感词拆成列表：去空行、去首尾空白，
// 单个词按 rune 截断到 50，最多保留 200 个（防止误粘贴超长文本拖慢每条消息的匹配）。
func splitBlockedWords(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		w := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if w == "" {
			continue
		}
		if r := []rune(w); len(r) > 50 {
			w = string(r[:50])
		}
		out = append(out, w)
		if len(out) >= 200 {
			break
		}
	}
	return out
}

// clampInt 解析整数并收敛到 [min,max]；空串或非法值用 fallback。
func clampInt(raw string, min, max, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

// SMTPConfig 用库里的配置构造 SMTPConfig。
// 第二个返回值表示库里是否真的配过（host 为空则调用方应保留环境变量配置）。
//
// logResetLink 由调用方传入：它只跟运行模式有关，不属于站长可改项。
func (s *SettingStore) SMTPConfig(logResetLink bool) (config.SMTPConfig, bool) {
	all, err := s.All()
	if err != nil {
		return config.SMTPConfig{}, false
	}
	host := all[KeySMTPHost]
	if host == "" {
		return config.SMTPConfig{}, false
	}
	port := all[KeySMTPPort]
	if port == "" {
		port = "465"
	}
	from := all[KeySMTPFrom]
	if from == "" {
		from = all[KeySMTPUser]
	}
	return config.SMTPConfig{
		Host:         host,
		Port:         port,
		User:         all[KeySMTPUser],
		Pass:         all[KeySMTPPass],
		From:         from,
		LogResetLink: logResetLink,
	}, true
}
