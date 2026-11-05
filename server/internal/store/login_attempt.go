package store

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// LoginAttemptStore 登录失败计数（按账号）。
type LoginAttemptStore struct {
	db *gorm.DB
}

func NewLoginAttemptStore(db *gorm.DB) *LoginAttemptStore {
	return &LoginAttemptStore{db: db}
}

// IsLocked 判断账号是否处于锁定中，返回剩余时长。
// 锁定期已过的记录顺手删掉，避免表里堆垃圾。
func (s *LoginAttemptStore) IsLocked(username string) (bool, time.Duration, error) {
	var row model.LoginAttempt
	err := s.db.Where("username = ?", username).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if row.LockedUntil == nil {
		return false, 0, nil
	}
	remaining := time.Until(*row.LockedUntil)
	if remaining <= 0 {
		_ = s.Reset(username)
		return false, 0, nil
	}
	return true, remaining, nil
}

// Fail 记一次登录失败；达到阈值就锁定并清零计数。
//
// 用 ON DUPLICATE KEY UPDATE + failed_count + 1 做原子自增：
// 并发失败请求不会互相覆盖导致少算次数。
func (s *LoginAttemptStore) Fail(username string, maxAttempts, lockMinutes int) error {
	now := time.Now()
	row := model.LoginAttempt{Username: username, FailedCount: 1, UpdatedAt: now}
	if err := s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "username"}},
		DoUpdates: clause.Assignments(map[string]any{
			"failed_count": gorm.Expr("failed_count + 1"),
			"updated_at":   now,
		}),
	}).Create(&row).Error; err != nil {
		return err
	}

	var cur model.LoginAttempt
	if err := s.db.Where("username = ?", username).First(&cur).Error; err != nil {
		return err
	}
	// 已经锁定中的不再重复设置（IsLocked 会在入口挡住，这里是并发兜底）
	if cur.LockedUntil != nil && cur.LockedUntil.After(now) {
		return nil
	}
	if cur.FailedCount >= maxAttempts {
		until := now.Add(time.Duration(lockMinutes) * time.Minute)
		return s.db.Model(&model.LoginAttempt{}).
			Where("username = ?", username).
			Updates(map[string]any{"locked_until": until, "failed_count": 0}).Error
	}
	return nil
}

// Reset 登录成功后清空计数。
func (s *LoginAttemptStore) Reset(username string) error {
	return s.db.Where("username = ?", username).Delete(&model.LoginAttempt{}).Error
}
