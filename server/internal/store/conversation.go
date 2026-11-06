package store

import (
	"crypto/rand"
	"encoding/hex"
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

// InviteMembers 批量拉人入群。与 JoinGroup 共用同一把会话行锁，
// 保证「数成员 → 判容量 → 写成员」在并发邀请/入群下不会突破上限。
// 已在群中的人会被忽略（幂等）；返回本次实际新增的用户 id。
func (s *ConversationStore) InviteMembers(convID uint, userIDs []uint, maxMembers int64) ([]uint, error) {
	var added []uint
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var conv model.Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conv, convID).Error; err != nil {
			return err
		}
		var existing []uint
		if err := tx.Model(&model.ConversationMember{}).
			Where("conversation_id = ? AND user_id IN ?", convID, userIDs).
			Pluck("user_id", &existing).Error; err != nil {
			return err
		}
		seen := make(map[uint]bool, len(existing))
		for _, id := range existing {
			seen[id] = true
		}
		pending := make([]uint, 0, len(userIDs))
		for _, id := range userIDs {
			if !seen[id] {
				pending = append(pending, id)
			}
		}
		if len(pending) == 0 {
			return nil
		}

		var n int64
		if err := tx.Model(&model.ConversationMember{}).
			Where("conversation_id = ?", convID).Count(&n).Error; err != nil {
			return err
		}
		// 整批放不下就整体拒绝，避免「邀请 5 人只进来 2 个」这种难以解释的部分成功
		if n+int64(len(pending)) > maxMembers {
			return ErrGroupFull
		}

		rows := make([]model.ConversationMember, 0, len(pending))
		for _, id := range pending {
			rows = append(rows, model.ConversationMember{
				ConversationID: convID, UserID: id, Role: "member",
			})
		}
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
		added = pending
		return nil
	})
	return added, err
}

// SetAvatar 设置群头像（空串表示恢复默认头像）。
func (s *ConversationStore) SetAvatar(convID uint, avatar string) error {
	var value any
	if avatar != "" {
		value = avatar
	}
	return s.db.Model(&model.Conversation{}).Where("id = ?", convID).Update("avatar", value).Error
}

// SetMemberRole 设置成员角色（owner 转让走 SetOwner；这里用于 admin/member）。
func (s *ConversationStore) SetMemberRole(convID, userID uint, role string) error {
	return s.db.Model(&model.ConversationMember{}).
		Where("conversation_id = ? AND user_id = ?", convID, userID).
		Update("role", role).Error
}

// SetMute 设置/解除禁言。until=nil 表示解除。
func (s *ConversationStore) SetMute(convID, userID uint, until *time.Time) error {
	return s.db.Model(&model.ConversationMember{}).
		Where("conversation_id = ? AND user_id = ?", convID, userID).
		Update("mute_until", until).Error
}

// RemoveMember 踢人 / 退群时删除成员行。
func (s *ConversationStore) RemoveMember(convID, userID uint) error {
	return s.db.Where("conversation_id = ? AND user_id = ?", convID, userID).
		Delete(&model.ConversationMember{}).Error
}

// Rename 群主改群名。
// SetPrefs 更新「我对这个会话」的个人设置（置顶 / 免打扰）。
//
// 存在 user_conversations 而不是 conversations：这是每人一份的偏好，
// 我置顶不代表别人也置顶。nil 表示该项不修改。
func (s *ConversationStore) SetPrefs(userID, convID uint, pinned, muted, archived *bool) error {
	updates := map[string]any{}
	if pinned != nil {
		updates["pinned"] = *pinned
	}
	if muted != nil {
		updates["muted"] = *muted
	}
	if archived != nil {
		updates["archived"] = *archived
	}
	if len(updates) == 0 {
		return nil
	}
	return s.db.Model(&model.UserConversation{}).
		Where("user_id = ? AND conversation_id = ?", userID, convID).
		Updates(updates).Error
}

// DeleteInbox 从该用户的会话列表移除（不退出群、不删除消息）。
// 之后该会话再来新消息时，UpsertInbox 会重新创建收件箱行。
func (s *ConversationStore) DeleteInbox(userID, convID uint) error {
	return s.db.Where("user_id = ? AND conversation_id = ?", userID, convID).
		Delete(&model.UserConversation{}).Error
}

// ClearHistory 清空该用户在该会话的聊天记录：设置水位并重置预览/未读。
func (s *ConversationStore) ClearHistory(userID, convID uint, upTo uint64) error {
	return s.db.Model(&model.UserConversation{}).
		Where("user_id = ? AND conversation_id = ?", userID, convID).
		Updates(map[string]any{
			"cleared_before_id":    upTo,
			"unread_count":         0,
			"mention_unread":       false,
			"last_read_message_id": upTo,
			"last_message_id":      nil,
			"last_message_at":      nil,
		}).Error
}

// LatestMessageID 会话当前最新消息 id（用于清空水位）。
func (s *ConversationStore) LatestMessageID(convID uint) (uint64, error) {
	var id uint64
	err := s.db.Model(&model.Message{}).Where("conversation_id = ?", convID).
		Select("COALESCE(MAX(id), 0)").Scan(&id).Error
	return id, err
}

// LatestVisibleMessage 会话当前最新可见消息的 id 与时间。
// 新成员加入 / 注册自动入群时用它初始化收件箱预览：不做的话列表里会显示
// 「暂无消息」，而打开会话又能看到历史，二者自相矛盾。
func (s *ConversationStore) LatestVisibleMessage(convID uint) (uint64, *time.Time, error) {
	var row struct {
		ID        uint64
		CreatedAt time.Time
	}
	err := s.db.Model(&model.Message{}).
		Select("id, created_at").
		Where("conversation_id = ? AND admin_deleted_at IS NULL", convID).
		Order("id DESC").
		Limit(1).
		Scan(&row).Error
	if err != nil || row.ID == 0 {
		return 0, nil, err
	}
	at := row.CreatedAt
	return row.ID, &at, nil
}

// SetMuteAll 群全员禁言开关。
func (s *ConversationStore) SetMuteAll(convID uint, muted bool) error {
	return s.db.Model(&model.Conversation{}).Where("id = ?", convID).
		Update("mute_all", muted).Error
}

// SetAnnouncement 设置群公告。content 为空表示撤销公告。
// 记录发布人与时间：前端要显示「谁在什么时候发的」。
func (s *ConversationStore) SetAnnouncement(convID, byUserID uint, content string) error {
	if content == "" {
		return s.db.Model(&model.Conversation{}).
			Where("id = ?", convID).
			Updates(map[string]any{
				"announcement":    nil,
				"announcement_at": nil,
				"announcement_by": nil,
			}).Error
	}
	now := time.Now()
	return s.db.Model(&model.Conversation{}).
		Where("id = ?", convID).
		Updates(map[string]any{
			"announcement":    content,
			"announcement_at": now,
			"announcement_by": byUserID,
		}).Error
}

// PrefsOf 读取会话的个人设置（置顶 / 免打扰）。
func (s *ConversationStore) PrefsOf(userID, convID uint) (pinned, muted, archived bool, err error) {
	var row struct {
		Pinned   bool
		Muted    bool
		Archived bool
	}
	err = s.db.Model(&model.UserConversation{}).
		Select("pinned, muted, archived").
		Where("user_id = ? AND conversation_id = ?", userID, convID).
		Scan(&row).Error
	return row.Pinned, row.Muted, row.Archived, err
}

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

// CreateInvite 生成群邀请链接。expiresAt=nil 表示永久有效。
func (s *ConversationStore) CreateInvite(convID, by uint, expiresAt *time.Time) (*model.ConversationInvite, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	inv := &model.ConversationInvite{
		Token:          hex.EncodeToString(b),
		ConversationID: convID,
		CreatedBy:      by,
		ExpiresAt:      expiresAt,
	}
	if err := s.db.Create(inv).Error; err != nil {
		return nil, err
	}
	return inv, nil
}

// InviteByToken 按 token 查邀请链接（不存在返回 nil,nil）。
func (s *ConversationStore) InviteByToken(token string) (*model.ConversationInvite, error) {
	var inv model.ConversationInvite
	err := s.db.Where("token = ?", token).First(&inv).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// OtherMemberID 单聊里除指定用户外的另一方 id（拉黑拦截用）。
func (s *ConversationStore) OtherMemberID(convID, userID uint) (uint, error) {
	var id uint
	err := s.db.Model(&model.ConversationMember{}).
		Where("conversation_id = ? AND user_id <> ?", convID, userID).
		Limit(1).
		Pluck("user_id", &id).Error
	return id, err
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
	ID                uint
	Type              int8
	Name              *string
	Avatar            string
	OwnerID           *uint
	LastMessageID     *uint64
	LastMessageAt     *time.Time
	LastReadMessageID uint64
	UnreadCount       int
	Pinned            bool
	Muted             bool
	Archived          bool
	MuteAll           bool
	MentionUnread     bool
	Announcement      *string
	AnnouncementAt    *time.Time
	AnnouncementBy    *uint
	AnnouncementWho   *string
}

// ListFor 用户会话列表：收件箱行 join 会话，再聚合预览 / 成员数 / peer。
func (s *ConversationStore) ListFor(userID uint) ([]model.ConversationView, error) {
	var rows []convRow
	err := s.db.Raw(`
		SELECT c.id, c.type, c.name, COALESCE(c.avatar, '') AS avatar, c.owner_id,
		       uc.last_message_id, uc.last_message_at, uc.last_read_message_id, uc.unread_count,
		       uc.pinned, uc.muted, uc.archived, uc.mention_unread,
		       c.mute_all,
		       c.announcement, c.announcement_at, c.announcement_by,
		       COALESCE(au.nickname, '') AS announcement_who
		FROM user_conversations uc
		JOIN conversations c ON c.id = uc.conversation_id
		LEFT JOIN users au ON au.id = c.announcement_by
		WHERE uc.user_id = ?
		ORDER BY uc.pinned DESC, uc.last_message_at DESC`, userID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	convIDs := make([]uint, 0, len(rows))
	rawLastIDs := make([]uint64, 0, len(rows))
	for _, r := range rows {
		convIDs = append(convIDs, r.ID)
		if r.LastMessageID != nil {
			rawLastIDs = append(rawLastIDs, *r.LastMessageID)
		}
	}

	// 「仅自己删除」：收件箱指向的最后一条若已被本人删除，回退到仍可见的最近一条，
	// 否则会话列表会继续显示一条聊天记录里已经看不到的消息。
	deleted, err := s.deletedLastIDs(userID, rawLastIDs)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].LastMessageID == nil || !deleted[*rows[i].LastMessageID] {
			continue
		}
		id, at, err := s.latestVisible(userID, rows[i].ID)
		if err != nil {
			return nil, err
		}
		if id == 0 {
			rows[i].LastMessageID = nil
			rows[i].LastMessageAt = nil
		} else {
			rows[i].LastMessageID = &id
			rows[i].LastMessageAt = at
		}
	}

	lastIDs := make([]uint64, 0, len(rows))
	for _, r := range rows {
		if r.LastMessageID != nil {
			lastIDs = append(lastIDs, *r.LastMessageID)
		}
	}

	previews, err := s.previewMap(lastIDs)
	if err != nil {
		return nil, err
	}
	// 只取「成员数」和「单聊对方」：
	// 旧实现把用户所有会话的全部成员行 JOIN users 拉回来，只为 len() 求成员数，
	// 20 个 200 人群就是 4000 行/次列表请求。成员摘要由详情面板按需拉取。
	counts, err := s.memberCounts(convIDs)
	if err != nil {
		return nil, err
	}
	singleIDs := make([]uint, 0, len(rows))
	for _, r := range rows {
		if r.Type == 1 {
			singleIDs = append(singleIDs, r.ID)
		}
	}
	peers, err := s.singlePeers(singleIDs, userID)
	if err != nil {
		return nil, err
	}

	out := make([]model.ConversationView, 0, len(rows))
	for _, r := range rows {
		v := model.ConversationView{
			ID:                r.ID,
			Type:              r.Type,
			Name:              r.Name,
			Avatar:            r.Avatar,
			OwnerID:           r.OwnerID,
			LastMessageID:     r.LastMessageID,
			LastMessageAt:     r.LastMessageAt,
			LastReadMessageID: r.LastReadMessageID,
			UnreadCount:       r.UnreadCount,
			Pinned:            r.Pinned,
			Muted:             r.Muted,
			Archived:          r.Archived,
			MuteAll:           r.MuteAll,
			MentionUnread:     r.MentionUnread,
		}
		if r.Announcement != nil {
			v.Announcement = *r.Announcement
		}
		v.AnnouncementAt = r.AnnouncementAt
		if r.AnnouncementWho != nil {
			v.AnnouncementBy = *r.AnnouncementWho
		}
		if r.LastMessageID != nil {
			if p, ok := previews[*r.LastMessageID]; ok {
				content := p.Content
				msgType := p.MsgType
				v.LastMessagePreview = &content
				v.LastMessageType = &msgType
			}
		}
		v.MemberCount = counts[r.ID]
		if r.Type == 1 {
			if p, ok := peers[r.ID]; ok {
				peer := p
				v.Peer = &peer
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// deletedLastIDs 返回这些消息里被该用户「仅自己删除」的集合。
func (s *ConversationStore) deletedLastIDs(userID uint, ids []uint64) (map[uint64]bool, error) {
	out := make(map[uint64]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var found []uint64
	if err := s.db.Model(&model.MessageDeletion{}).
		Where("user_id = ? AND message_id IN ?", userID, ids).
		Pluck("message_id", &found).Error; err != nil {
		return nil, err
	}
	for _, id := range found {
		out[id] = true
	}
	// 管理员删除的消息对所有成员都不可见，同样需要回退列表预览
	var adminDeleted []uint64
	if err := s.db.Model(&model.Message{}).
		Where("id IN ? AND admin_deleted_at IS NOT NULL", ids).
		Pluck("id", &adminDeleted).Error; err != nil {
		return nil, err
	}
	for _, id := range adminDeleted {
		out[id] = true
	}
	return out, nil
}

// latestVisible 该用户在该会话仍可见的最近一条消息（已排除本人删除的消息）。
func (s *ConversationStore) latestVisible(userID, convID uint) (uint64, *time.Time, error) {
	var last struct {
		ID        uint64
		CreatedAt time.Time
	}
	err := s.db.Raw(`
		SELECT m.id, m.created_at FROM messages m
		LEFT JOIN message_deletions d ON d.message_id = m.id AND d.user_id = ?
		WHERE m.conversation_id = ? AND d.message_id IS NULL AND m.admin_deleted_at IS NULL
		ORDER BY m.id DESC LIMIT 1`, userID, convID).Scan(&last).Error
	if err != nil || last.ID == 0 {
		return 0, nil, err
	}
	at := last.CreatedAt
	return last.ID, &at, nil
}

// previewMap 消息预览（撤回显示固定文案）。
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
			// 不写死中文：文案由前端按当前语言渲染（英文界面刷新后不该冒出中文）。
			// MsgType 用合成的 "recalled"，前端据此显示「消息已撤回」。
			out[r.ID] = previewInfo{Content: "", MsgType: "recalled"}
			continue
		}
		// 图片/语音的 content 是文件地址，直接当预览会把 URL 显示在列表里
		out[r.ID] = previewInfo{Content: r.Content, MsgType: r.MsgType}
	}
	return out, nil
}

// memberCounts convID -> 成员数（一条 GROUP BY，避免物化全部成员行）。
func (s *ConversationStore) memberCounts(convIDs []uint) (map[uint]int, error) {
	out := make(map[uint]int, len(convIDs))
	if len(convIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ConversationID uint `gorm:"column:conversation_id"`
		N              int  `gorm:"column:n"`
	}
	if err := s.db.Table("conversation_members").
		Select("conversation_id, COUNT(*) AS n").
		Where("conversation_id IN ?", convIDs).
		Group("conversation_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ConversationID] = r.N
	}
	return out, nil
}

// singlePeers convID -> 单聊对方资料（群聊不查，群成员由详情面板按需拉取）。
func (s *ConversationStore) singlePeers(convIDs []uint, userID uint) (map[uint]model.UserBrief, error) {
	out := make(map[uint]model.UserBrief, len(convIDs))
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
		Where("cm.conversation_id IN ? AND cm.user_id <> ?", convIDs, userID).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ConversationID] = model.UserBrief{
			ID: r.ID, Username: r.Username, Nickname: r.Nickname, AvatarColor: r.AvatarColor, Avatar: r.Avatar,
		}
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
	// 一次 GROUP BY 拿全部成员数：旧实现是「命中 N 个群就查 N 次 COUNT」，
	// 20 个结果就是 21 次数据库往返
	ids := make([]uint, 0, len(cs))
	for i := range cs {
		ids = append(ids, cs[i].ID)
	}
	counts, err := s.memberCounts(ids)
	if err != nil {
		return nil, err
	}
	out := make([]model.ConversationView, 0, len(cs))
	for i := range cs {
		c := cs[i]
		// 头像也一并下发：搜索结果里直接显示群头像
		out = append(out, model.ConversationView{
			ID: c.ID, Type: c.Type, Name: c.Name, Avatar: c.Avatar,
			OwnerID: c.OwnerID, MemberCount: counts[c.ID],
		})
	}
	return out, nil
}

// MembersView 成员列表（含用户摘要）。
func (s *ConversationStore) MembersView(convID uint) ([]model.MemberView, error) {
	var rows []struct {
		UserID      uint       `gorm:"column:user_id"`
		Role        string     `gorm:"column:role"`
		MutedUntil  *time.Time `gorm:"column:mute_until"`
		JoinedAt    time.Time  `gorm:"column:joined_at"`
		ID          uint       `gorm:"column:id"`
		Username    string     `gorm:"column:username"`
		Nickname    string     `gorm:"column:nickname"`
		AvatarColor string     `gorm:"column:avatar_color"`
		Avatar      string     `gorm:"column:avatar"`
	}
	err := s.db.Table("conversation_members cm").
		Select("cm.user_id, cm.role, cm.mute_until, cm.joined_at, u.id, u.username, u.nickname, u.avatar_color, u.avatar").
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
			UserID:     r.UserID,
			Role:       r.Role,
			JoinedAt:   r.JoinedAt,
			MutedUntil: r.MutedUntil,
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
