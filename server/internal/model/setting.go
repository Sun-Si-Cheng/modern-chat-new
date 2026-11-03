package model

import "time"

// SystemSetting 运行时配置（键值对）。
//
// 优先级：**数据库 > 环境变量**。环境变量仍是启动时的兜底（也方便容器化部署），
// 后台改过的值会落库并在重启后继续生效。
//
// 含密钥的项（SMTP 密码、Redis 密码）以 AES-256-GCM 加密存储，
// 密钥由 JWT_SECRET 派生——见 store/secretbox.go。
type SystemSetting struct {
	Key string `gorm:"primaryKey;size:64" json:"key"`
	// Value 明文配置值；Encrypted 为 true 时存的是密文（base64）
	Value     string    `gorm:"type:text;not null" json:"-"`
	Encrypted bool      `gorm:"not null;default:false" json:"-"`
	UpdatedBy uint      `gorm:"not null;default:0" json:"updatedBy"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
}

// TableName 避免 GORM 复数化后的表名与本项目其它表风格不一致。
func (SystemSetting) TableName() string { return "system_settings" }
