package store

import (
	"sync"

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

	// 对象存储：当前版本**没有文件上传功能**（头像只接受 URL 字符串），
	// 这组配置会被保存，但不会生效。保留是为了后续接入上传时无需再改表结构。
	KeyStorageEndpoint  = "storage.endpoint"
	KeyStorageRegion    = "storage.region"
	KeyStorageBucket    = "storage.bucket"
	KeyStorageAccessKey = "storage.accessKey"
	KeyStorageSecretKey = "storage.secretKey" // 加密
	// PublicBase 对象存储的对外访问前缀（CDN 域名）。留空则按 endpoint/bucket 拼。
	KeyStoragePublicBase = "storage.publicBase"
)

var knownKeys = map[string]bool{
	KeySMTPHost: true, KeySMTPPort: true, KeySMTPUser: true, KeySMTPPass: true, KeySMTPFrom: true,
	KeyRedisAddr: true, KeyRedisPass: true, KeyRedisDB: true,
	KeyStorageEndpoint: true, KeyStorageRegion: true, KeyStorageBucket: true,
	KeyStorageAccessKey: true, KeyStorageSecretKey: true, KeyStoragePublicBase: true,
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
	return nil
}

// Invalidate 丢弃缓存（例如外部直接改库后）。
func (s *SettingStore) Invalidate() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
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
