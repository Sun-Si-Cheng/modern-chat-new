package store

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// MessageStore 消息与收件箱。
type MessageStore struct{ db *gorm.DB }

func NewMessageStore(db *gorm.DB) *MessageStore { return &MessageStore{db: db} }

func (s *MessageStore) Create(m *model.Message) error { return s.db.Create(m).Error }

func (s *MessageStore) ByID(id uint) (*model.Message, error) {
	var m model.Message
	err := s.db.First(&m, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ByClientMsg 幂等回查。
func (s *MessageStore) ByClientMsg(convID uint, clientMsgID string) (*model.Message, error) {
	var m model.Message
	err := s.db.Where("conversation_id = ? AND client_msg_id = ?", convID, clientMsgID).First(&m).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// PageBefore 游标分页：按 id 降序，beforeID=0 表示从最新开始。返回 (页, 是否还有更早)。
func (s *MessageStore) PageBefore(convID uint, beforeID uint64, limit int) ([]model.Message, bool, error) {
	q := s.db.Where("conversation_id = ?", convID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []model.Message
	if err := q.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, false, err
	}
	hasMore := false
	if len(rows) > limit {
		rows = rows[:limit]
		hasMore = true
	}
	return rows, hasMore, nil
}

// MarkRecalled 撤回软标记。
func (s *MessageStore) MarkRecalled(id uint) error {
	return s.db.Model(&model.Message{}).Where("id = ?", id).Update("is_recalled", true).Error
}

// UpsertInbox 批量更新收件箱行的 last_message_*（新成员行不存在则插入，unread 保持初始值）。
func (s *MessageStore) UpsertInbox(rows []model.UserConversation) error {
	if len(rows) == 0 {
		return nil
	}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "conversation_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_message_id", "last_message_at"}),
	}).Create(&rows).Error
}

// BumpUnread 对除发送者外的成员未读 +1。
// 以 last_message_id = 新消息 id 为条件，确保只对刚 upsert 的行生效。
func (s *MessageStore) BumpUnread(convID, senderID uint, msgID uint64) error {
	return s.db.Model(&model.UserConversation{}).
		Where("conversation_id = ? AND user_id <> ? AND last_message_id = ?", convID, senderID, msgID).
		UpdateColumn("unread_count", gorm.Expr("unread_count + 1")).Error
}

// MarkRead 上报已读：清未读、推进 last_read_message_id（只前进不后退）。
// 返回受影响行数（0 表示重复上报，无需广播）。
func (s *MessageStore) MarkRead(userID, convID uint, upToID uint64) (int64, error) {
	res := s.db.Model(&model.UserConversation{}).
		Where("user_id = ? AND conversation_id = ? AND last_read_message_id < ?", userID, convID, upToID).
		Updates(map[string]any{"unread_count": 0, "last_read_message_id": upToID})
	return res.RowsAffected, res.Error
}

// RemoveInbox 删除成员的收件箱行（退群 / 被踢）。
func (s *MessageStore) RemoveInbox(userID, convID uint) error {
	return s.db.Where("user_id = ? AND conversation_id = ?", userID, convID).
		Delete(&model.UserConversation{}).Error
}

// RecallUnreadAdjust 撤回未读消息时回退未读（仅未读到该消息的人）。
func (s *MessageStore) RecallUnreadAdjust(convID, senderID uint, msgID uint64) error {
	return s.db.Model(&model.UserConversation{}).
		Where(
			"conversation_id = ? AND user_id <> ? AND unread_count > 0 AND last_read_message_id < ?",
			convID, senderID, msgID,
		).
		UpdateColumn("unread_count", gorm.Expr("GREATEST(unread_count - 1, 0)")).Error
}

// InboxOf 单个成员的收件箱行。
func (s *MessageStore) InboxOf(userID, convID uint) (*model.UserConversation, error) {
	var uc model.UserConversation
	err := s.db.Where("user_id = ? AND conversation_id = ?", userID, convID).First(&uc).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &uc, nil
}
