package store

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// PushStore Web Push 订阅。
type PushStore struct{ db *gorm.DB }

func NewPushStore(db *gorm.DB) *PushStore { return &PushStore{db: db} }

// Upsert 保存订阅：同一端点重复订阅（刷新/重装）时更新密钥，避免重复推送。
func (s *PushStore) Upsert(sub *model.PushSubscription) error {
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "endpoint"}},
		DoUpdates: clause.Assignments(map[string]any{"p256dh": sub.P256dh, "auth": sub.Auth, "user_id": sub.UserID}),
	}).Create(sub).Error
}

// Remove 删除某个端点（用户关闭推送或端点已失效）。
func (s *PushStore) Remove(endpoint string) error {
	return s.db.Where("endpoint = ?", endpoint).Delete(&model.PushSubscription{}).Error
}

// ForUser 某用户的全部订阅端点。
func (s *PushStore) ForUser(userID uint) ([]model.PushSubscription, error) {
	var rows []model.PushSubscription
	err := s.db.Where("user_id = ?", userID).Find(&rows).Error
	return rows, err
}
