package model

import "time"

// TwofaRecoveryCode 两步验证恢复码（每个用户 8 个，单次有效）。
//
// 只存 sha256 哈希（明文仅在启用时展示一次），一个码被消费即标记 used_at。
// 唯一索引防同一明文码跨用户/跨次重复。
type TwofaRecoveryCode struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	UserID    uint       `gorm:"index" json:"userId"`
	CodeHash  string     `gorm:"size:64;uniqueIndex" json:"-"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
	CreatedAt time.Time  `json:"-"`
}

// 开发模式走 store.AutoMigrate；生产由迁移 0027 建表，两边字段保持一致。
