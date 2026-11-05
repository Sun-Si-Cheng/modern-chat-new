package store

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// ModerationStore 治理相关：拉黑、举报、管理员删除消息。
type ModerationStore struct{ db *gorm.DB }

func NewModerationStore(db *gorm.DB) *ModerationStore { return &ModerationStore{db: db} }

// Block 拉黑（幂等）。
func (s *ModerationStore) Block(blockerID, blockedID uint) error {
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.UserBlock{BlockerID: blockerID, BlockedID: blockedID}).Error
}

// Unblock 取消拉黑（幂等）。
func (s *ModerationStore) Unblock(blockerID, blockedID uint) error {
	return s.db.Where("blocker_id = ? AND blocked_id = ?", blockerID, blockedID).
		Delete(&model.UserBlock{}).Error
}

// IsBlocked blockerID 是否拉黑了 blockedID（单向）。
func (s *ModerationStore) IsBlocked(blockerID, blockedID uint) (bool, error) {
	var n int64
	err := s.db.Model(&model.UserBlock{}).
		Where("blocker_id = ? AND blocked_id = ?", blockerID, blockedID).Count(&n).Error
	return n > 0, err
}

// BlockedEither 两人之间是否存在任一方向的拉黑关系。
// 业务上用于拦截单聊发送、发起会话与好友申请（拉黑即互不打扰）。
func (s *ModerationStore) BlockedEither(a, b uint) (bool, error) {
	var n int64
	err := s.db.Model(&model.UserBlock{}).
		Where("(blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)", a, b, b, a).
		Count(&n).Error
	return n > 0, err
}

// Report 举报消息。返回本次是否真的新增（重复举报返回 false）。
func (s *ModerationStore) Report(reporterID, messageID uint, reason string) (bool, error) {
	res := s.db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.MessageReport{ReporterID: reporterID, MessageID: messageID, Reason: reason})
	return res.RowsAffected > 0, res.Error
}

// AdminDeleteMessage 标记管理员删除。返回本次是否真的从「未删除」变为「已删除」。
// 消息本体保留（便于审计与申诉），仅通过查询过滤让所有成员看不到。
func (s *ModerationStore) AdminDeleteMessage(messageID, by uint) (bool, error) {
	now := time.Now()
	res := s.db.Model(&model.Message{}).
		Where("id = ? AND admin_deleted_at IS NULL", messageID).
		Updates(map[string]any{"admin_deleted_at": now, "admin_deleted_by": by})
	return res.RowsAffected > 0, res.Error
}
