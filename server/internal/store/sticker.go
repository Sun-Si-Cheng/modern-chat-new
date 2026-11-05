package store

import (
	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// StickerStore 个人表情库。
type StickerStore struct {
	db *gorm.DB
}

func NewStickerStore(db *gorm.DB) *StickerStore { return &StickerStore{db: db} }

// List 某用户的全部表情，按加入时间倒序（最近用的排在前面）。
func (s *StickerStore) List(userID uint) ([]model.Sticker, error) {
	rows := make([]model.Sticker, 0, 32)
	err := s.db.Where("user_id = ?", userID).
		Order("id DESC").
		Find(&rows).Error
	return rows, err
}

// Count 当前表情数量（用于上限判断）。
func (s *StickerStore) Count(userID uint) (int64, error) {
	var n int64
	err := s.db.Model(&model.Sticker{}).Where("user_id = ?", userID).Count(&n).Error
	return n, err
}

// Add 收藏一个表情。同一用户同一 URL 不重复（幂等：重复添加直接返回已有记录）。
func (s *StickerStore) Add(userID uint, url string) (*model.Sticker, error) {
	var exist model.Sticker
	err := s.db.Where("user_id = ? AND url = ?", userID, url).First(&exist).Error
	if err == nil {
		return &exist, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	row := &model.Sticker{UserID: userID, URL: url}
	if err := s.db.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// Delete 删除自己的表情。带 user_id 条件：不能删别人的。
func (s *StickerStore) Delete(userID, id uint) (bool, error) {
	res := s.db.Where("id = ? AND user_id = ?", id, userID).Delete(&model.Sticker{})
	return res.RowsAffected > 0, res.Error
}
