package store

import (
	"sort"
	"strings"
	"time"

	"log/slog"

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

// AfterID 增量补齐：返回 id > afterID 的消息（升序），用于离线期间按游标补齐。
func (s *MessageStore) AfterID(convID, viewerID uint, afterID uint64, limit int) ([]model.Message, bool, error) {
	var rows []model.Message
	if err := s.db.Where(
		"conversation_id = ? AND id > ? AND admin_deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM message_deletions d WHERE d.user_id = ? AND d.message_id = messages.id) AND messages.id > COALESCE((SELECT cleared_before_id FROM user_conversations WHERE user_id = ? AND conversation_id = ?), 0)",
		convID, afterID, viewerID, viewerID, convID,
	).Order("id ASC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, false, err
	}
	hasMore := false
	if len(rows) > limit {
		rows = rows[:limit]
		hasMore = true
	}
	// 与首屏分页保持一致：补齐的消息同样要带引用摘要，否则断线重连后
	// 增量消息里的引用块会渲染成空白，直到刷新才恢复。
	if err := s.AttachReply(rows); err != nil {
		return nil, false, err
	}
	return rows, hasMore, nil
}

// PageBefore 游标分页：按 id 降序，beforeID=0 表示从最新开始。返回 (页, 是否还有更早)。
// viewerID 的「仅自己删除」记录会被排除，避免分页里再次出现被删消息。
func (s *MessageStore) PageBefore(convID, viewerID uint, beforeID uint64, limit int) ([]model.Message, bool, error) {
	q := s.db.Where(
		"conversation_id = ? AND admin_deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM message_deletions d WHERE d.user_id = ? AND d.message_id = messages.id) AND messages.id > COALESCE((SELECT cleared_before_id FROM user_conversations WHERE user_id = ? AND conversation_id = ?), 0)",
		convID, viewerID, viewerID, convID,
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
	// 引用摘要在查询层统一装配，前端拿到就能直接渲染引用块
	if err := s.AttachReply(rows); err != nil {
		return nil, false, err
	}
	return rows, hasMore, nil
}

// AttachReply 为一批消息装配引用摘要。
//
// 批量查而不是逐条查：消息列表一次 30-50 条，N+1 会让翻页变慢。
// 被引用消息若已撤回，摘要里直接标记，前端渲染成「引用内容已撤回」。
func (s *MessageStore) AttachReply(rows []model.Message) error {
	ids := make([]uint64, 0, len(rows))
	for _, m := range rows {
		if m.ReplyToID != nil {
			ids = append(ids, *m.ReplyToID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	var targets []struct {
		ID         uint64
		SenderID   uint
		MsgType    string
		Content    string
		IsRecalled bool
		Nickname   string
	}
	if err := s.db.Table("messages AS m").
		Select("m.id, m.sender_id, m.msg_type, m.content, m.is_recalled, COALESCE(u.nickname, '') AS nickname").
		Joins("LEFT JOIN users u ON u.id = m.sender_id").
		Where("m.id IN ?", ids).
		Scan(&targets).Error; err != nil {
		return err
	}

	briefs := make(map[uint64]model.ReplyBrief, len(targets))
	for _, t := range targets {
		content := t.Content
		if t.IsRecalled {
			content = ""
		} else if r := []rune(content); len(r) > 60 {
			// 引用块只做提示，截断避免撑破气泡
			content = string(r[:60]) + "…"
		}
		briefs[t.ID] = model.ReplyBrief{
			ID:             t.ID,
			SenderID:       t.SenderID,
			SenderNickname: t.Nickname,
			MsgType:        t.MsgType,
			Content:        content,
			IsRecalled:     t.IsRecalled,
		}
	}
	for i := range rows {
		if rows[i].ReplyToID == nil {
			continue
		}
		if b, ok := briefs[*rows[i].ReplyToID]; ok {
			brief := b
			rows[i].ReplyTo = &brief
		}
	}
	return nil
}

// MarkMention 把这些用户的收件箱标记为「有未读 @我」。
// 免打扰的会话也靠它显示「有人@我」，所以不能只靠未读数。
func (s *MessageStore) MarkMention(convID uint, userIDs []uint) error {
	return markMention(s.db, convID, userIDs)
}

func markMention(db *gorm.DB, convID uint, userIDs []uint) error {
	if len(userIDs) == 0 {
		return nil
	}
	return db.Model(&model.UserConversation{}).
		Where("conversation_id = ? AND user_id IN ?", convID, userIDs).
		UpdateColumn("mention_unread", true).Error
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

// CreateWithInbox 在一个事务里完成「写消息 + 全员收件箱 last/时间推进 +
// 未读 +1 + @ 标记」。任一步失败整体回滚，调用方不再广播——
// 否则会出现消息已入库、收件箱/未读没更新的永久不一致。
func (s *MessageStore) CreateWithInbox(m *model.Message, memberIDs []uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		rows := make([]model.UserConversation, 0, len(memberIDs))
		for _, uid := range memberIDs {
			id := uint64(m.ID)
			rows = append(rows, model.UserConversation{
				UserID:         uid,
				ConversationID: m.ConversationID,
				LastMessageID:  &id,
				LastMessageAt:  &m.CreatedAt,
			})
		}
		if err := upsertInbox(tx, rows); err != nil {
			return err
		}
		if err := bumpUnread(tx, m.ConversationID, m.SenderID, uint64(m.ID)); err != nil {
			return err
		}
		return markMention(tx, m.ConversationID, m.Mentions)
	})
}

// UpsertInbox 批量更新收件箱行的 last_message_*（新成员行不存在则插入，unread 保持初始值）。
// 用 GREATEST 保证 last_message_id 与 last_message_at 只增不减，避免并发/乱序消息
// 让较小值覆盖较大值，导致列表排序回退、预览显示旧消息。
func (s *MessageStore) UpsertInbox(rows []model.UserConversation) error {
	if len(rows) == 0 {
		return nil
	}
	return upsertInbox(s.db, rows)
}

func upsertInbox(db *gorm.DB, rows []model.UserConversation) error {
	if len(rows) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{
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
	return bumpUnread(s.db, convID, senderID, msgID)
}

func bumpUnread(db *gorm.DB, convID, senderID uint, msgID uint64) error {
	return db.Model(&model.UserConversation{}).
		Where("conversation_id = ? AND user_id <> ? AND last_read_message_id < ?", convID, senderID, msgID).
		UpdateColumn("unread_count", gorm.Expr("unread_count + 1")).Error
}

// MarkRead 上报已读：清未读、推进 last_read_message_id（只前进不后退）。
// 返回受影响行数（0 表示重复上报，无需广播）。
func (s *MessageStore) MarkRead(userID, convID uint, upToID uint64) (int64, error) {
	// 条件里带上 `unread_count > 0`：正常路径下 last_read 与 unread 一起更新，
	// 但若两者出现不一致（历史数据 / 手工修过库 / 端上补报），只判 last_read 会跳过更新，
	// 结果是红点永远消不掉。带上未读条件即可兜住这种情况，且不影响幂等。
	// last_read 用 GREATEST 只进不退：否则补报一个更小的 upToID 会把游标拽回去，
	// 之后的历史消息会重新变成「未读」，分页取新消息也会重复。
	res := s.db.Model(&model.UserConversation{}).
		Where(
			"user_id = ? AND conversation_id = ? AND (last_read_message_id < ? OR unread_count > 0)",
			userID, convID, upToID,
		).
		Updates(map[string]any{
			"unread_count":         0,
			"last_read_message_id": gorm.Expr("GREATEST(COALESCE(last_read_message_id, 0), ?)", upToID),
			// 已读即清掉「有人@我」：否则提示会一直挂着
			"mention_unread": false,
		})
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
			WHERE m.conversation_id = ? AND d.message_id IS NULL AND m.admin_deleted_at IS NULL
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
		Where("f.user_id = ? AND m.admin_deleted_at IS NULL", userID).
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

// Search 消息搜索：只搜索调用者已加入的会话，排除已撤回与本人「仅自己删除」的消息。
// convID=0 表示全局搜索。≥2 字符的关键词走 FULLTEXT ngram 短语匹配（中文按连续
// 子串匹配），单字符回退 LIKE（ngram 最小 token 是 2 字符）；FULLTEXT 异常时也
// 回退 LIKE，保证搜索永远可用。
func (s *MessageStore) Search(userID, convID uint, kw string, limit int) ([]model.SearchHitView, error) {
	rows, err := s.searchOnce(userID, convID, kw, limit, true)
	if err != nil {
		slog.Warn("message search fulltext failed, fallback to LIKE", "err", err.Error())
		rows, err = s.searchOnce(userID, convID, kw, limit, false)
	}
	if err != nil {
		return nil, err
	}
	// 单聊没有群名：用对方昵称补全会话名
	singleIDs := make([]uint, 0, len(rows))
	for _, r := range rows {
		if r.ConvType == 1 && r.ConversationID > 0 {
			singleIDs = append(singleIDs, r.ConversationID)
		}
	}
	if len(singleIDs) > 0 {
		var peers []struct {
			ConversationID uint   `gorm:"column:conversation_id"`
			Nickname       string `gorm:"column:nickname"`
		}
		if err := s.db.Table("conversation_members cm").
			Select("cm.conversation_id, u.nickname").
			Joins("JOIN users u ON u.id = cm.user_id").
			Where("cm.conversation_id IN ? AND cm.user_id <> ?", singleIDs, userID).
			Scan(&peers).Error; err != nil {
			return nil, err
		}
		names := make(map[uint]string, len(peers))
		for _, p := range peers {
			names[p.ConversationID] = p.Nickname
		}
		for i := range rows {
			if rows[i].ConvType == 1 {
				rows[i].ConvName = names[rows[i].ConversationID]
			}
		}
	}
	return rows, nil
}

// searchOnce 执行一次搜索查询。fulltext=true 且关键词 ≥2 字符时走 FULLTEXT
// 短语匹配（保留 LIKE 的连续性语义），否则 LIKE。
func (s *MessageStore) searchOnce(userID, convID uint, kw string, limit int, fulltext bool) ([]model.SearchHitView, error) {
	var cond string
	var args []any
	if fulltext && len([]rune(kw)) >= 2 {
		cond = "m.is_recalled = ? AND m.admin_deleted_at IS NULL AND MATCH(m.content) AGAINST(? IN BOOLEAN MODE)"
		args = []any{false, fulltextPhrase(kw)}
	} else {
		cond = "m.is_recalled = ? AND m.admin_deleted_at IS NULL AND m.content LIKE ?"
		args = []any{false, "%" + likeEscape(kw) + "%"}
	}
	q := s.db.Table("messages m").
		Select(`m.id AS message_id, m.conversation_id, c.type AS conv_type,
			COALESCE(c.name, '') AS conv_name, m.sender_id, u.nickname AS sender_nickname,
			m.msg_type, m.content, m.created_at`).
		Joins("JOIN conversations c ON c.id = m.conversation_id").
		Joins("JOIN users u ON u.id = m.sender_id").
		Where(cond, args...).
		Where("EXISTS (SELECT 1 FROM conversation_members cm WHERE cm.conversation_id = m.conversation_id AND cm.user_id = ?)", userID).
		Where("NOT EXISTS (SELECT 1 FROM message_deletions d WHERE d.user_id = ? AND d.message_id = m.id)", userID)
	if convID > 0 {
		q = q.Where("m.conversation_id = ?", convID)
	}
	var rows []model.SearchHitView
	err := q.Order("m.id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

// fulltextPhrase 把关键词包装成 BOOLEAN MODE 短语：引号内是连续短语，近似
// LIKE 的子串语义；去掉关键词里的引号，避免提前闭合短语。
func fulltextPhrase(kw string) string {
	return `"` + strings.ReplaceAll(kw, `"`, "") + `"`
}

// UpdateContent 编辑消息正文并记录编辑时间。调用方先校验发送者、类型与时限。
func (s *MessageStore) UpdateContent(id uint, content string) (time.Time, error) {
	now := time.Now()
	res := s.db.Model(&model.Message{}).
		Where("id = ? AND is_recalled = ?", id, false).
		Updates(map[string]any{"content": content, "edited_at": now})
	return now, res.Error
}

// SetReaction 设置某人对某条消息的表情（同一人只保留一个）。
// 返回设置前该用户的表情（可能为空），供前端精确扣减旧表情的计数。
func (s *MessageStore) SetReaction(messageID, userID uint, emoji string) (string, error) {
	var prev model.MessageReaction
	err := s.db.Where("message_id = ? AND user_id = ?", messageID, userID).First(&prev).Error
	notFound := err == gorm.ErrRecordNotFound
	if err != nil && !notFound {
		return "", err
	}
	if !notFound && prev.Emoji == emoji {
		return prev.Emoji, nil
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "message_id"}, {Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{"emoji": emoji, "created_at": time.Now()}),
	}).Create(&model.MessageReaction{
		MessageID: messageID, UserID: userID, Emoji: emoji,
	}).Error; err != nil {
		return "", err
	}
	if notFound {
		return "", nil
	}
	return prev.Emoji, nil
}

// RemoveReaction 取消表情回应（幂等）。
func (s *MessageStore) RemoveReaction(messageID, userID uint) error {
	return s.db.Where("message_id = ? AND user_id = ?", messageID, userID).
		Delete(&model.MessageReaction{}).Error
}

// AttachReactions 为一批消息装配表情聚合（计数 + 我是否回应过）。
// 历史分页与断线重连都走这里；WS 回显的新消息没有表情，无需装配。
func (s *MessageStore) AttachReactions(msgs []model.Message, viewerID uint) error {
	if len(msgs) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(msgs))
	for _, m := range msgs {
		if m.ID > 0 {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var rows []model.MessageReaction
	if err := s.db.Where("message_id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	counts := make(map[uint]map[string]int, len(ids))
	mine := make(map[uint]string, len(ids))
	for _, r := range rows {
		if counts[r.MessageID] == nil {
			counts[r.MessageID] = map[string]int{}
		}
		counts[r.MessageID][r.Emoji]++
		if r.UserID == viewerID {
			mine[r.MessageID] = r.Emoji
		}
	}
	for i := range msgs {
		set := counts[msgs[i].ID]
		if len(set) == 0 {
			continue
		}
		views := make([]model.ReactionView, 0, len(set))
		for emoji, n := range set {
			views = append(views, model.ReactionView{
				Emoji: emoji, Count: n, Mine: mine[msgs[i].ID] == emoji,
			})
		}
		sort.Slice(views, func(a, b int) bool {
			if views[a].Count != views[b].Count {
				return views[a].Count > views[b].Count
			}
			return views[a].Emoji < views[b].Emoji
		})
		msgs[i].Reactions = views
	}
	return nil
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
