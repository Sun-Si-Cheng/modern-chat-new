package model

import "time"

// AdminAudit 站长操作审计记录（后台写操作留痕）。
type AdminAudit struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	AdminID    uint      `gorm:"index;not null" json:"adminId"`
	Action     string    `gorm:"size:40;not null" json:"action"`
	TargetType string    `gorm:"size:20;not null" json:"targetType"`
	TargetID   uint      `gorm:"not null;default:0" json:"targetId"`
	TargetName string    `gorm:"size:120;not null;default:''" json:"targetName"`
	Detail     string    `gorm:"size:500;not null;default:''" json:"detail"`
	IP         string    `gorm:"size:64;not null;default:''" json:"ip"`
	CreatedAt  time.Time `gorm:"index" json:"createdAt"`
}

// TableName 固定表名（避免 gorm 复数化差异）。
func (AdminAudit) TableName() string { return "admin_audits" }
