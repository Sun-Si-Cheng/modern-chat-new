package store

import (
	"time"

	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// ConversationStore 会话与成员。
type ConversationStore struct{ db *gorm.DB }

func NewConversationStore(db *gorm.DB) *ConversationStore { return &ConversationStore{db: db} }

func (s *ConversationStore) Create(c *model.Conversation) error { return s.db.Create(c).Error }

func (s *ConversationStore) ByID(id uint) (*model.Conversation, error) {
	var c model.Conversation
	err := s.db.First(&c, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// MemberOf 某用户在某会话的成员行。
func (s *ConversationStore) MemberOf(convID, userID uint) (*model.ConversationMember, error) {
	var m model.ConversationMember
	err := s.db.Where("conversation_id = ? AND user_id = ?", convID, userID).First(&m).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// AddMembers 批量插入成员（调用方先做重复校验）。
func (s *ConversationStore) AddMembers(rows []model.ConversationMember) error {
	if len(rows) == 0 {
		return nil
	}
	return s.db.Create(&rows).Error
}

// CountMembers 成员数。
func (s *ConversationStore) CountMembers(convID uint) (int64, error) {
	var n int64
	err := s.db.Model(&model.ConversationMember{}).Where("conversation_id = ?", convID).Count(&n).Error
	return n, err
}

// RemoveMember 踢人 / 退群时删除成员行。
func (s *ConversationStore) RemoveMember(convID, userID uint) error {
	return s.db.Where("conversation_id = ? AND user_id = ?", convID, userID).
		Delete(&model.ConversationMember{}).Error
}

// Rename 群主改群名。
func (s *ConversationStore) Rename(convID uint, name string) error {
	return s.db.Model(&model.Conversation{}).Where("id = ?", convID).Update("name", name).Error
}

// SetOwner 转让群主：conversations.owner_id 与两侧成员 role 必须在同一事务内一起改，
// 否则前端（读 ownerId）与后端（判 members.role）会得出不同结论。
func (s *ConversationStore) SetOwner(convID, newOwnerID, oldOwnerID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Conversation{}).
			Where("id = ?", convID).
			Update("owner_id", newOwnerID).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ConversationMember{}).
			Where("conversation_id = ? AND user_id = ?", convID, newOwnerID).
			Update("role", "owner").Error; err != nil {
			return err
		}
		return tx.Model(&model.ConversationMember{}).
			Where("conversation_id = ? AND user_id = ?", convID, oldOwnerID).
			Update("role", "member").Error
	})
}

// MemberUserIDs 全部成员 id（fan-out 用）。
func (s *ConversationStore) MemberUserIDs(convID uint) ([]uint, error) {
	var ids []uint
	err := s.db.Model(&model.ConversationMember{}).
		Where("conversation_id = ?", convID).
		Pluck("user_id", &ids).Error
	return ids, err
}

// ConvIDsOfUser 用户所在的全部会话 id。
func (s *ConversationStore) ConvIDsOfUser(userID uint) ([]uint, error) {
	var ids []uint
	err := s.db.Model(&model.ConversationMember{}).
		Where("user_id = ?", userID).
		Pluck("conversation_id", &ids).Error
	return ids, err
}

// MemberIDsOfConvs 一组会话的全部成员（去重）。
func (s *ConversationStore) MemberIDsOfConvs(convIDs []uint) ([]uint, error) {
	var ids []uint
	if len(convIDs) == 0 {
		return ids, nil
	}
	err := s.db.Model(&model.ConversationMember{}).
		Where("conversation_id IN ?", convIDs).
		Distinct().
		Pluck("user_id", &ids).Error
	return ids, err
}

// SingleExists 查找两人已存在的 type=1 会话（单聊幂等复用）。
func (s *ConversationStore) SingleExists(a, b uint) (*model.Conversation, error) {
	var c model.Conversation
	err := s.db.Raw(`
		SELECT c.* FROM conversations c
		JOIN conversation_members m1 ON m1.conversation_id = c.id AND m1.user_id = ?
		JOIN conversation_members m2 ON m2.conversation_id = c.id AND m2.user_id = ?
		WHERE c.type = 1
		LIMIT 1`, a, b).Scan(&c).Error
	if err != nil {
		return nil, err
	}
	if c.ID == 0 {
		return nil, nil
	}
	return &c, nil
}

// ---- 列表组装 ----

type convRow struct {
	ID            uint
	Type          int8
	Name          *string
	OwnerID       *uint
	LastMessageID *uint64
	LastMessageAt *time.Time
	UnreadCount   int
}

// ListFor 用户会话列表：收件箱行 join 会话，再聚合预览 / 成员数 / peer。
func (s *ConversationStore) ListFor(userID uint) ([]model.ConversationView, error) {
	var rows []convRow
	err := s.db.Raw(`
		SELECT c.id, c.type, c.name, c.owner_id,
		       uc.last_message_id, uc.last_message_at, uc.unread_count
		FROM user_conversations uc
		JOIN conversations c ON c.id = uc.conversation_id
		WHERE uc.user_id = ?
		ORDER BY uc.last_message_at DESC`, userID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	convIDs := make([]uint, 0, len(rows))
	lastIDs := make([]uint64, 0, len(rows))
	for _, r := range rows {
		convIDs = append(convIDs, r.ID)
		if r.LastMessageID != nil {
			lastIDs = append(lastIDs, *r.LastMessageID)
		}
	}

	previews, err := s.previewMap(lastIDs)
	if err != nil {
		return nil, err
	}
	members, err := s.memberBriefs(convIDs)
	if err != nil {
		return nil, err
	}

	out := make([]model.ConversationView, 0, len(rows))
	for _, r := range rows {
		v := model.ConversationView{
			ID:            r.ID,
			Type:          r.Type,
			Name:          r.Name,
			OwnerID:       r.OwnerID,
			LastMessageID: r.LastMessageID,
			LastMessageAt: r.LastMessageAt,
			UnreadCount:   r.UnreadCount,
		}
		if r.LastMessageID != nil {
			if p, ok := previews[*r.LastMessageID]; ok {
				v.LastMessagePreview = &p
			}
		}
		ms := members[r.ID]
		v.MemberCount = len(ms)
		if r.Type == 1 {
			for _, m := range ms {
				if m.ID != userID {
					peer := m
					v.Peer = &peer
					break
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// previewMap 消息预览（撤回显示固定文案）。
func (s *ConversationStore) previewMap(ids []uint64) (map[uint64]string, error) {
	out := make(map[uint64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID         uint64
		Content    string
		IsRecalled bool
	}
	if err := s.db.Model(&model.Message{}).
		Select("id, content, is_recalled").
		Where("id IN ?", ids).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.IsRecalled {
			out[r.ID] = "消息已撤回"
		} else {
			out[r.ID] = r.Content
		}
	}
	return out, nil
}

// memberBriefs convID -> 成员摘要列表。
func (s *ConversationStore) memberBriefs(convIDs []uint) (map[uint][]model.UserBrief, error) {
	out := make(map[uint][]model.UserBrief, len(convIDs))
	if len(convIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ConversationID uint   `gorm:"column:conversation_id"`
		ID             uint   `gorm:"column:id"`
		Username       string `gorm:"column:username"`
		Nickname       string `gorm:"column:nickname"`
		AvatarColor    string `gorm:"column:avatar_color"`
		Avatar         string `gorm:"column:avatar"`
	}
	if err := s.db.Table("conversation_members cm").
		Select("cm.conversation_id, u.id, u.username, u.nickname, u.avatar_color, u.avatar").
		Joins("JOIN users u ON u.id = cm.user_id").
		Where("cm.conversation_id IN ?", convIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ConversationID] = append(out[r.ConversationID], model.UserBrief{
			ID: r.ID, Username: r.Username, Nickname: r.Nickname, AvatarColor: r.AvatarColor, Avatar: r.Avatar,
		})
	}
	return out, nil
}

// SearchGroups 群名模糊搜索（公开可加入）。
func (s *ConversationStore) SearchGroups(kw string, limit int) ([]model.ConversationView, error) {
	var cs []model.Conversation
	pat := "%" + likeEscape(kw) + "%"
	if err := s.db.Where("type = 2 AND name LIKE ?", pat).Order("id ASC").Limit(limit).Find(&cs).Error; err != nil {
		return nil, err
	}
	out := make([]model.ConversationView, 0, len(cs))
	for i := range cs {
		n, err := s.CountMembers(cs[i].ID)
		if err != nil {
			return nil, err
		}
		c := cs[i]
		out = append(out, model.ConversationView{ID: c.ID, Type: c.Type, Name: c.Name, OwnerID: c.OwnerID, MemberCount: int(n)})
	}
	return out, nil
}

// MembersView 成员列表（含用户摘要）。
func (s *ConversationStore) MembersView(convID uint) ([]model.MemberView, error) {
	var rows []struct {
		UserID      uint      `gorm:"column:user_id"`
		Role        string    `gorm:"column:role"`
		JoinedAt    time.Time `gorm:"column:joined_at"`
		ID          uint      `gorm:"column:id"`
		Username    string    `gorm:"column:username"`
		Nickname    string    `gorm:"column:nickname"`
		AvatarColor string    `gorm:"column:avatar_color"`
		Avatar      string    `gorm:"column:avatar"`
	}
	err := s.db.Table("conversation_members cm").
		Select("cm.user_id, cm.role, cm.joined_at, u.id, u.username, u.nickname, u.avatar_color, u.avatar").
		Joins("JOIN users u ON u.id = cm.user_id").
		Where("cm.conversation_id = ?", convID).
		Order("cm.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]model.MemberView, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.MemberView{
			UserID:   r.UserID,
			Role:     r.Role,
			JoinedAt: r.JoinedAt,
			User: model.UserBrief{
				ID:          r.ID,
				Username:    r.Username,
				Nickname:    r.Nickname,
				AvatarColor: r.AvatarColor,
				Avatar:      r.Avatar,
			},
		})
	}
	return out, nil
}
