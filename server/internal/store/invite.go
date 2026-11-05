package store

import (
	"time"

	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// InviteStore 注册邀请码。
type InviteStore struct {
	db *gorm.DB
}

func NewInviteStore(db *gorm.DB) *InviteStore { return &InviteStore{db: db} }

// List 按创建时间倒序列出邀请码，附带创建者昵称。
func (s *InviteStore) List(limit int) ([]model.InviteCode, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows := make([]model.InviteCode, 0, limit)
	var raw []struct {
		model.InviteCode
		Nickname string
	}
	err := s.db.Table("invite_codes i").
		Select("i.*, COALESCE(u.nickname, '') AS nickname").
		Joins("LEFT JOIN users u ON u.id = i.created_by").
		Order("i.id DESC").
		Limit(limit).
		Scan(&raw).Error
	if err != nil {
		return nil, err
	}
	for _, r := range raw {
		row := r.InviteCode
		row.Creator = r.Nickname
		rows = append(rows, row)
	}
	return rows, nil
}

// Create 新建一个邀请码。
func (s *InviteStore) Create(row *model.InviteCode) error {
	return s.db.Create(row).Error
}

// Consume 原子核销一次。
//
// 全部有效性条件都放在 WHERE 里由数据库判定：名额未满、未禁用、未过期。
// 返回 false 表示这个码不可用（无效 / 用完 / 过期 / 被禁用）。
func (s *InviteStore) Consume(code string) (bool, error) {
	res := s.db.Model(&model.InviteCode{}).
		Where("code = ? AND disabled = ? AND uses < max_uses AND (expires_at IS NULL OR expires_at > ?)",
			code, false, time.Now()).
		UpdateColumn("uses", gorm.Expr("uses + 1"))
	return res.RowsAffected > 0, res.Error
}

// Refund 归还一次名额。仅用于「核销成功但随后注册失败」的补偿，
// 避免用户因为用户名重复这类原因白白损失一个邀请名额。
func (s *InviteStore) Refund(code string) error {
	return s.db.Model(&model.InviteCode{}).
		Where("code = ? AND uses > 0", code).
		UpdateColumn("uses", gorm.Expr("uses - 1")).Error
}

// Disable 停用 / 启用一个邀请码。
func (s *InviteStore) Disable(id uint, disabled bool) (bool, error) {
	res := s.db.Model(&model.InviteCode{}).
		Where("id = ?", id).
		UpdateColumn("disabled", disabled)
	return res.RowsAffected > 0, res.Error
}
