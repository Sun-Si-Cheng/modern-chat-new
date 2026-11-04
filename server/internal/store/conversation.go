package store

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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

// ErrGroupFull 群成员已达上限。
var ErrGroupFull = errors.New("group is full")

// JoinGroup 原子入群：锁会话行 → 数成员 → 插入成员行，全程在同一事务内。
// 修复缺陷：原实现先 CountMembers 再 AddMembers，两步之间没有互斥，
// 并发入群可让成员数突破 200 上限；这里用 SELECT ... FOR UPDATE 把「判容量」
// 与「写入」串行化到同一会话行上。
func (s *ConversationStore) JoinGroup(convID, userID uint, maxMembers int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var conv model.Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conv, convID).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&model.ConversationMember{}).
			Where("conversation_id = ?", convID).Count(&n).Error; err != nil {
			return err
		}
		if n >= maxMembers {
			return ErrGroupFull
		}
		return tx.Create(&model.ConversationMember{
			ConversationID: convID, UserID: userID, Role: "member",
		}).Error
	})
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
				content := p.Content
				msgType := p.MsgType
				v.LastMessagePreview = &content
				v.LastMessageType = &msgType
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
// previewInfo 会话列表里的一行预览：内容 + 类型（前端据此把 URL 换成「图片/语音」）
type previewInfo struct {
	Content string
	MsgType string
}

func (s *ConversationStore) previewMap(ids []uint64) (map[uint64]previewInfo, error) {
	out := make(map[uint64]previewInfo, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID         uint64
		Content    string
		MsgType    string
		IsRecalled bool
	}
	if err := s.db.Model(&model.Message{}).
		Select("id, content, msg_type, is_recalled").
		Where("id IN ?", ids).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.IsRecalled {
			out[r.ID] = previewInfo{Content: "消息已撤回", MsgType: "text"}
			continue
		}
		// 图片/语音的 content 是文件地址，直接当预览会把 URL 显示在列表里
		out[r.ID] = previewInfo{Content: r.Content, MsgType: r.MsgType}
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

// CountByType 单聊 / 群聊数量。
func (s *ConversationStore) CountByType() (single, group int64, err error) {
	var rows []struct {
		Type  int
		Total int64
	}
	if err = s.db.Model(&model.Conversation{}).
		Select("type, COUNT(*) AS total").
		Group("type").
		Scan(&rows).Error; err != nil {
		return 0, 0, err
	}
	for _, r := range rows {
		switch r.Type {
		case 1:
			single = r.Total
		case 2:
			group = r.Total
		}
	}
	return single, group, nil
}

// CountForUser 某用户参与的会话数。
func (s *ConversationStore) CountForUser(userID uint) (int64, error) {
	var n int64
	err := s.db.Model(&model.ConversationMember{}).
		Where("user_id = ?", userID).Count(&n).Error
	return n, err
}

// AddMembersIgnoreDuplicate 批量加成员，已存在则跳过（公告把全站用户拉进系统会话时需要）。
func (s *ConversationStore) AddMembersIgnoreDuplicate(rows []model.ConversationMember) error {
	if len(rows) == 0 {
		return nil
	}
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// systemConvType 系统通知会话（单聊=1 群聊=2）。
const systemConvType = 3

// EnsureSystemConversation 取系统通知会话，不存在则创建。
func (s *ConversationStore) EnsureSystemConversation(name string) (*model.Conversation, error) {
	var c model.Conversation
	err := s.db.Where("type = ?", systemConvType).Order("id ASC").First(&c).Error
	if err == nil {
		return &c, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	c = model.Conversation{Type: systemConvType, Name: &name}
	if err := s.db.Create(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// MaxGroupMembers 群成员上限（控制 fan-out 写放大）。
const MaxGroupMembers int64 = 200

// DefaultGroupName 初始公开群名称：所有人都可以加入。
const DefaultGroupName = "公共聊天室"

// EnsureDefaultGroup 取初始公开群（type=2、无群主），不存在则创建。
// created=true 表示本次新建，调用方据此决定是否把现有用户批量加入。
// 不设置 owner：任何成员都能自由退群，也不会出现「群主退不了」的例外。
func (s *ConversationStore) EnsureDefaultGroup() (*model.Conversation, bool, error) {
	var c model.Conversation
	err := s.db.Where("type = 2 AND name = ?", DefaultGroupName).Order("id ASC").First(&c).Error
	if err == nil {
		return &c, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	name := DefaultGroupName
	c = model.Conversation{Type: 2, Name: &name}
	if err := s.db.Create(&c).Error; err != nil {
		return nil, false, err
	}
	return &c, true, nil
}

// BootstrapDefaultGroup 启动时创建初始公开群，并把现有用户批量加入。
// 只在「首次创建」时批量加入：用户之后主动退群，不会被下次启动重新拉回。
func BootstrapDefaultGroup(db *gorm.DB) error {
	convs := NewConversationStore(db)
	users := NewUserStore(db)
	msgs := NewMessageStore(db)

	conv, created, err := convs.EnsureDefaultGroup()
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	ids, err := users.AllIDs()
	if err != nil {
		return err
	}
	// 初始群同样受成员上限约束，超出部分仍可后续手动加入（若已满则需等空位）。
	if int64(len(ids)) > MaxGroupMembers {
		ids = ids[:MaxGroupMembers]
	}
	if len(ids) == 0 {
		return nil
	}
	members := make([]model.ConversationMember, 0, len(ids))
	inbox := make([]model.UserConversation, 0, len(ids))
	for _, id := range ids {
		members = append(members, model.ConversationMember{
			ConversationID: conv.ID, UserID: id, Role: "member",
		})
		inbox = append(inbox, model.UserConversation{UserID: id, ConversationID: conv.ID})
	}
	if err := convs.AddMembersIgnoreDuplicate(members); err != nil {
		return err
	}
	return msgs.UpsertInbox(inbox)
}
