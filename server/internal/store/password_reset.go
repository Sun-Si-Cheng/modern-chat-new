package store

import (
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

// Use 在同一事务内标记令牌已用并执行业务（写新密码 + tv+1）。
func (s *PasswordResetStore) Use(pr *model.PasswordReset, fn func(tx *gorm.DB) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(pr).Update("used", true).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
