package model

import "time"

// Plugin 用户安装的插件（installed_by 隔离；global_disabled 为站长全局下架开关）。
// permissions / allowed_hosts 以 JSON 数组字符串存储。
type Plugin struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Name           string    `gorm:"size:30;index" json:"name"`
	Version        string    `gorm:"size:16" json:"version"`
	Description    string    `gorm:"size:255" json:"description"`
	CodeText       string    `gorm:"type:mediumtext" json:"-"`
	Permissions    string    `gorm:"type:text" json:"-"`
	AllowedHosts   string    `gorm:"type:text" json:"-"`
	Enabled        bool      `gorm:"default:false" json:"enabled"`
	GlobalDisabled bool      `gorm:"default:false" json:"globalDisabled"`
	InstalledBy    uint      `gorm:"index" json:"installedBy"`
	CreatedAt      time.Time `json:"createdAt"`
}
