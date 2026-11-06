package store

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// PasswordResetStore 找回密码令牌。
type PasswordResetStore struct{ db *gorm.DB }

func NewPasswordResetStore(db *gorm.DB) *PasswordResetStore { return &PasswordResetStore{db: db} }

func (s *PasswordResetStore) Create(pr *model.PasswordReset) error { return s.db.Create(pr).Error }

// Valid 查有效令牌：未使用且未过期。
func (s *PasswordResetStore) Valid(token string) (*model.PasswordReset, error) {
	var pr model.PasswordReset
	err := s.db.Where("token = ? AND used = ? AND expires_at > ?", token, false, time.Now()).
		First(&pr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pr, nil
}

// ErrResetTokenUsed 令牌已被使用/已过期：并发提交同一链接时只有一个成功。
var ErrResetTokenUsed = errors.New("password reset token already used")

// Use 原子地核销令牌并执行业务（写新密码 + tv+1）。
//
// 修复缺陷：原实现直接 `Update("used", true)`，不检查令牌是否仍然有效，
// 两个并发请求拿着同一个链接都能通过 Valid 并各自改密码（TOCTOU）。
// 现在在事务里做条件更新，RowsAffected=0 即说明已被另一请求核销。
func (s *PasswordResetStore) Use(pr *model.PasswordReset, fn func(tx *gorm.DB) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.PasswordReset{}).
			Where("id = ? AND used = ? AND expires_at > ?", pr.ID, false, time.Now()).
			Update("used", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrResetTokenUsed
		}
		return fn(tx)
	})
}
