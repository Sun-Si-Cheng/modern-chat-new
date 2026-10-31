// Package model 全部 GORM 模型（表结构见 docs/设计文档.md §5）。
package model

import "time"

// User 用户。JSON 输出即对外的 User 实体（camelCase）。
type User struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	Username      string     `gorm:"size:20;uniqueIndex" json:"username"`
	PasswordHash  string     `gorm:"size:60" json:"-"`
	Nickname      string     `gorm:"size:20" json:"nickname"`
	AvatarColor   string     `gorm:"size:16" json:"avatarColor"`
	// Avatar 自定义头像：前端压缩后的 data URL（≤256KB），空表示使用默认头像
	Avatar string `gorm:"type:mediumtext" json:"avatar,omitempty"`
	Email         *string    `gorm:"size:254;uniqueIndex" json:"email,omitempty"`
	EmailVerified bool       `json:"-"`
	Role          string     `gorm:"size:10;default:user" json:"role"` // user | admin
	TokenVersion  int64      `gorm:"not null;default:1" json:"-"`
	BannedUntil   *time.Time `json:"bannedUntil,omitempty"`
	BanReason     *string    `gorm:"size:255" json:"banReason,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"-"`
}

// IsBanned 当前是否处于封禁期。
func (u *User) IsBanned() bool {
	return u.BannedUntil != nil && u.BannedUntil.After(time.Now())
}

// 前端默认头像取色板（与 TDesign 色系协调）。
var AvatarPalette = []string{
	"#0052d9", "#2ba471", "#d54941", "#ed7b2f", "#ca8d04",
	"#4b5b76", "#2f5f8a", "#6f42c1", "#8a3f3f", "#3d7f6f",
}

// PickColor 按 id 取模取头像色。
func PickColor(id uint) string {
	return AvatarPalette[int(id)%len(AvatarPalette)]
}
