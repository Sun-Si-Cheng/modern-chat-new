package store

import (
	"time"

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
// viewerID 的「仅自己删除」记录会被排除，避免分页里再次出现被删消息。
func (s *MessageStore) PageBefore(convID, viewerID uint, beforeID uint64, limit int) ([]model.Message, bool, error) {
	q := s.db.Where(
		"conversation_id = ? AND NOT EXISTS (SELECT 1 FROM message_deletions d WHERE d.user_id = ? AND d.message_id = messages.id)",
		convID, viewerID,
	)
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

// MarkRecalled 撤回软标记。返回本次是否真的由「未撤回」变为「已撤回」。
//
// 幂等性很关键：调用方据此决定是否回退未读数与广播 recall 帧。
// 原实现无条件 UPDATE，重复撤回（连点、重发帧）会让 RecallUnreadAdjust
// 对同一消息再扣一次未读，把接收方其它消息的未读计数吃掉。
// MarkRecalled 标记撤回并记录撤回时间。
//
// 时间必须由服务端产生并落库：前端需要跨刷新、跨设备一致地判断
// 「撤回提示显示满 3 分钟了吗」，用客户端时钟会在多端之间打架。
// 返回值第二个是撤回时间，供 WS 广播帧带上。
func (s *MessageStore) MarkRecalled(id uint) (bool, time.Time, error) {
	now := time.Now()
	res := s.db.Model(&model.Message{}).
		Where("id = ? AND is_recalled = ?", id, false).
		Updates(map[string]any{"is_recalled": true, "recalled_at": now})
	return res.RowsAffected > 0, now, res.Error
}

// UpsertInbox 批量更新收件箱行的 last_message_*（新成员行不存在则插入，unread 保持初始值）。
// 修复缺陷：并发/乱序消息会让较小的 last_message_id 覆盖较大的值，导致会话列表排序
// 回退、预览显示旧消息。这里用 GREATEST 保证 last_message_id 与 last_message_at 只增不减。
func (s *MessageStore) UpsertInbox(rows []model.UserConversation) error {
	if len(rows) == 0 {
		return nil
	}
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "conversation_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			// 外层再 COALESCE 一次：新值为 NULL 时 GREATEST 会得到 NULL，
			// 那样会把已有的历史值抹掉，因此回退到旧值。
			"last_message_id": gorm.Expr(
				"COALESCE(GREATEST(COALESCE(last_message_id, 0), COALESCE(VALUES(last_message_id), 0)), last_message_id)"),
			"last_message_at": gorm.Expr(
				"COALESCE(GREATEST(COALESCE(last_message_at, VALUES(last_message_at)), VALUES(last_message_at)), last_message_at)"),
		}),
	}).Create(&rows).Error
}

// BumpUnread 对除发送者外的成员未读 +1。
// 修复缺陷：原条件 last_message_id = 新消息 id 在并发 upsert 下会漏计/错计
// （upsert 先写入了更大的 id 时该条件不成立）。改为「该用户尚未读到这条消息」
// 的语义条件：last_read_message_id < msgID；未读只增不减，重复执行也不会回退。
func (s *MessageStore) BumpUnread(convID, senderID uint, msgID uint64) error {
	return s.db.Model(&model.UserConversation{}).
		Where("conversation_id = ? AND user_id <> ? AND last_read_message_id < ?", convID, senderID, msgID).
		UpdateColumn("unread_count", gorm.Expr("unread_count + 1")).Error
}

// MarkRead 上报已读：清未读、推进 last_read_message_id（只前进不后退）。
// 返回受影响行数（0 表示重复上报，无需广播）。
func (s *MessageStore) MarkRead(userID, convID uint, upToID uint64) (int64, error) {
	// 条件里带上 `unread_count > 0`：正常路径下 last_read 与 unread 一起更新，
	// 但若两者出现不一致（历史数据 / 手工修过库 / 端上补报），只判 last_read 会跳过更新，
	// 结果是红点永远消不掉。带上未读条件即可兜住这种情况，且不影响幂等。
	res := s.db.Model(&model.UserConversation{}).
		Where(
			"user_id = ? AND conversation_id = ? AND (last_read_message_id < ? OR unread_count > 0)",
			userID, convID, upToID,
		).
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

// DeleteForUser 「仅自己删除」：写入删除标记，并回退该用户的收件箱预览/未读。
// 消息本体不删除，对方仍可见；同一消息重复删除是幂等的。
func (s *MessageStore) DeleteForUser(userID uint, msg *model.Message) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&model.MessageDeletion{UserID: userID, MessageID: msg.ID}).Error; err != nil {
			return err
		}
		// 删掉的是自己尚未读过的他人消息：未读 -1（与撤回的处理口径一致）
		if msg.SenderID != userID {
			if err := tx.Model(&model.UserConversation{}).
				Where(
					"user_id = ? AND conversation_id = ? AND unread_count > 0 AND last_read_message_id < ?",
					userID, msg.ConversationID, msg.ID,
				).
				UpdateColumn("unread_count", gorm.Expr("GREATEST(unread_count - 1, 0)")).Error; err != nil {
				return err
			}
		}
		// 回退收件箱的「最后一条消息」到该用户仍然可见的最近一条
		var last struct {
			ID        uint64
			CreatedAt time.Time
		}
		if err := tx.Raw(`
			SELECT m.id, m.created_at FROM messages m
			LEFT JOIN message_deletions d ON d.message_id = m.id AND d.user_id = ?
			WHERE m.conversation_id = ? AND d.message_id IS NULL
			ORDER BY m.id DESC LIMIT 1`, userID, msg.ConversationID).Scan(&last).Error; err != nil {
			return err
		}
		updates := map[string]any{"last_message_id": nil, "last_message_at": nil}
		if last.ID > 0 {
			updates["last_message_id"] = last.ID
			updates["last_message_at"] = last.CreatedAt
		}
		return tx.Model(&model.UserConversation{}).
			Where("user_id = ? AND conversation_id = ?", userID, msg.ConversationID).
			Updates(updates).Error
	})
}

// Favorite 收藏消息（幂等）。
func (s *MessageStore) Favorite(userID uint, messageID uint) error {
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.MessageFavorite{UserID: userID, MessageID: messageID}).Error
}

// Unfavorite 取消收藏（幂等）。
func (s *MessageStore) Unfavorite(userID uint, messageID uint) error {
	return s.db.Where("user_id = ? AND message_id = ?", userID, messageID).
		Delete(&model.MessageFavorite{}).Error
}

// Favorites 收藏列表（按收藏时间倒序）。单聊没有群名，用对方昵称作为会话名。
func (s *MessageStore) Favorites(userID uint, limit int) ([]model.FavoriteView, error) {
	// 必须初始化成空切片而不是 nil：Go 的 nil slice 序列化成 JSON 是 null，
	// 前端拿到 null 后 `items.map(...)` 直接抛错（收藏夹打不开）。
	rows := make([]model.FavoriteView, 0, limit)
	if err := s.db.Table("message_favorites f").
		Select(`f.message_id, m.conversation_id, c.type AS conv_type,
			COALESCE(c.name, '') AS conv_name, m.sender_id, u.nickname AS sender_nickname,
			m.msg_type, m.content, m.is_recalled, m.created_at, f.created_at AS favorited_at`).
		Joins("JOIN messages m ON m.id = f.message_id").
		Joins("JOIN conversations c ON c.id = m.conversation_id").
		Joins("JOIN users u ON u.id = m.sender_id").
		Where("f.user_id = ?", userID).
		Order("f.created_at DESC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	// 单聊会话名：对方昵称
	convIDs := make([]uint, 0, len(rows))
	for _, r := range rows {
		if r.ConvType == 1 && r.ConversationID > 0 {
			convIDs = append(convIDs, r.ConversationID)
		}
	}
	if len(convIDs) > 0 {
		var peers []struct {
			ConversationID uint   `gorm:"column:conversation_id"`
			Nickname       string `gorm:"column:nickname"`
		}
		if err := s.db.Table("conversation_members cm").
			Select("cm.conversation_id, u.nickname").
			Joins("JOIN users u ON u.id = cm.user_id").
			Where("cm.conversation_id IN ? AND cm.user_id <> ?", convIDs, userID).
			Scan(&peers).Error; err != nil {
			return nil, err
		}
		peerNames := make(map[uint]string, len(peers))
		for _, p := range peers {
			if peerNames[p.ConversationID] == "" {
				peerNames[p.ConversationID] = p.Nickname
			}
		}
		for i := range rows {
			if rows[i].ConvType == 1 {
				rows[i].ConvName = peerNames[rows[i].ConversationID]
			}
		}
	}
	return rows, nil
}

// Count 消息总数（含已撤回）。
func (s *MessageStore) Count() (int64, error) {
	var n int64
	err := s.db.Model(&model.Message{}).Count(&n).Error
	return n, err
}

// CountToday 今日新增消息数。
func (s *MessageStore) CountToday() (int64, error) {
	var n int64
	err := s.db.Model(&model.Message{}).Where("created_at >= ?", startOfToday()).Count(&n).Error
	return n, err
}

// CountBySender 某用户发送的消息数（含已撤回）。
func (s *MessageStore) CountBySender(userID uint) (int64, error) {
	var n int64
	err := s.db.Model(&model.Message{}).Where("sender_id = ?", userID).Count(&n).Error
	return n, err
}
