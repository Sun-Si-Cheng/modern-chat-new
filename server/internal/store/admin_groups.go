package store

import (
	"time"

	"gorm.io/gorm"
)

// 后台群聊管理只读查询（docs/旧站迁移方案.md §8）。
//
// conversations.type：1=单聊、2=群聊。后台只暴露群聊，单聊消息对管理员也不可见。
const adminGroupConversationType int8 = 2

// adminGroupOwnerNameSQL 群主展示名：昵称优先，昵称为空回退用户名。
// 列表与详情共用同一段 SQL，保证两处口径一致。
const adminGroupOwnerNameSQL = "COALESCE(NULLIF(u.nickname, ''), u.username, '')"

// AdminGroupRow 后台群聊列表行（JSON 字段即 §8 契约）。
type AdminGroupRow struct {
	ID           uint      `json:"id"`
	Name         string    `json:"name"`
	OwnerID      uint      `json:"ownerId"`
	OwnerName    string    `json:"ownerName"`
	MemberCount  int       `json:"memberCount"`
	MessageCount int       `json:"messageCount"`
	CreatedAt    time.Time `json:"createdAt"`
}

// AdminGroupMemberRow 群成员行。username/nickname 原样返回，由前端决定展示哪个。
type AdminGroupMemberRow struct {
	UserID   uint      `json:"userId"`
	Username string    `json:"username"`
	Nickname string    `json:"nickname"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
}

// AdminGroupDetail 群详情（§8 中为顶层对象）。不存在或不是群聊时返回 (nil, nil)。
type AdminGroupDetail struct {
	ID           uint      `json:"id"`
	Name         string    `json:"name"`
	OwnerID      uint      `json:"ownerId"`
	OwnerName    string    `json:"ownerName"`
	MemberCount  int       `json:"memberCount"`
	MessageCount int       `json:"messageCount"`
	CreatedAt    time.Time `json:"createdAt"`
	Announcement string    `json:"announcement"`
	// gorm:"-" 标记 Members 不是数据库列：详情先查群行再单独查成员，避免 GORM 把
	// 该切片当成关联关系解析（否则 Find 时报 "invalid field found for ... Members"）。
	Members []AdminGroupMemberRow `json:"members" gorm:"-"`
}

// AdminGroupMessageRow 后台群消息行。
//
// 与成员端消息视图的差别：不过滤 admin_deleted_at（管理员要能看见被删内容，
// 前端按 adminDeleted 置灰）；senderName 昵称优先、回退用户名。
type AdminGroupMessageRow struct {
	ID           uint      `json:"id"`
	SenderID     uint      `json:"senderId"`
	SenderName   string    `json:"senderName"`
	Content      string    `json:"content"`
	MsgType      string    `json:"msgType"`
	CreatedAt    time.Time `json:"createdAt"`
	IsRecalled   bool      `json:"isRecalled"`
	AdminDeleted bool      `json:"adminDeleted"`
}

// adminGroupFilter 列表/计数的统一筛选条件（q 模糊匹配群名，已由调用方 trim）。
func adminGroupFilter(q *gorm.DB, keyword string) *gorm.DB {
	q = q.Where("c.type = ?", adminGroupConversationType)
	if keyword != "" {
		q = q.Where("c.name LIKE ?", "%"+likeEscape(keyword)+"%")
	}
	return q
}

// ListAdminGroups 后台群聊分页列表，返回当页行与总数（keyword 为空表示全部）。
//
// 成员数、消息数用相关子查询在同一条 SQL 里取：翻一页仍然只有
// 「1 条 COUNT + 1 条列表」两条查询，不会按群逐个 COUNT（避免 N+1）。
func (s *ConversationStore) ListAdminGroups(keyword string, page, size int) ([]AdminGroupRow, int64, error) {
	var total int64
	if err := adminGroupFilter(s.db.Table("conversations c"), keyword).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	rows := make([]AdminGroupRow, 0, size)
	err := adminGroupFilter(s.db.Table("conversations c"), keyword).
		Select("c.id AS id, COALESCE(c.name, '') AS name, " +
			"COALESCE(c.owner_id, 0) AS owner_id, " +
			adminGroupOwnerNameSQL + " AS owner_name, " +
			"(SELECT COUNT(*) FROM conversation_members cm WHERE cm.conversation_id = c.id) AS member_count, " +
			"(SELECT COUNT(*) FROM messages m WHERE m.conversation_id = c.id) AS message_count, " +
			"c.created_at AS created_at").
		Joins("LEFT JOIN users u ON u.id = c.owner_id").
		Order("c.id DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// AdminGroupDetail 群详情：1 条群信息 + 1 条成员列表，共两条查询。
// 不存在或 type != 2 返回 (nil, nil)，由 handler 统一映射 404。
func (s *ConversationStore) AdminGroupDetail(convID uint) (*AdminGroupDetail, error) {
	var rows []AdminGroupDetail
	err := s.db.Table("conversations c").
		Select("c.id AS id, COALESCE(c.name, '') AS name, "+
			"COALESCE(c.owner_id, 0) AS owner_id, "+
			adminGroupOwnerNameSQL+" AS owner_name, "+
			"(SELECT COUNT(*) FROM conversation_members cm WHERE cm.conversation_id = c.id) AS member_count, "+
			"(SELECT COUNT(*) FROM messages m WHERE m.conversation_id = c.id) AS message_count, "+
			"c.created_at AS created_at, COALESCE(c.announcement, '') AS announcement").
		Joins("LEFT JOIN users u ON u.id = c.owner_id").
		Where("c.id = ? AND c.type = ?", convID, adminGroupConversationType).
		Limit(1).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	members := make([]AdminGroupMemberRow, 0, 16)
	err = s.db.Table("conversation_members cm").
		Select("cm.user_id AS user_id, COALESCE(u.username, '') AS username, "+
			"COALESCE(u.nickname, '') AS nickname, cm.role AS role, cm.joined_at AS joined_at").
		Joins("LEFT JOIN users u ON u.id = cm.user_id").
		Where("cm.conversation_id = ?", convID).
		Order("cm.id ASC").
		Find(&members).Error
	if err != nil {
		return nil, err
	}

	detail := rows[0]
	detail.Members = members
	return &detail, nil
}

// AdminGroupMessages 群消息游标分页（后台视角，新→旧）。
//
// beforeID>0 时取 id 更小的更旧消息；0 表示从最新开始。keyword 同时匹配
// 消息内容与发送者用户名。额外多取一条用于判断 hasMore。
func (s *MessageStore) AdminGroupMessages(convID uint, beforeID uint64, keyword string, size int) ([]AdminGroupMessageRow, bool, error) {
	q := s.db.Table("messages m").
		Select("m.id AS id, m.sender_id AS sender_id, "+
			"COALESCE(NULLIF(u.nickname, ''), u.username, '') AS sender_name, "+
			"COALESCE(m.content, '') AS content, COALESCE(m.msg_type, 'text') AS msg_type, "+
			"m.created_at AS created_at, m.is_recalled AS is_recalled, "+
			"(m.admin_deleted_at IS NOT NULL) AS admin_deleted").
		Joins("LEFT JOIN users u ON u.id = m.sender_id").
		Where("m.conversation_id = ?", convID)
	if beforeID > 0 {
		q = q.Where("m.id < ?", beforeID)
	}
	if keyword != "" {
		pat := "%" + likeEscape(keyword) + "%"
		q = q.Where("m.content LIKE ? OR u.username LIKE ?", pat, pat)
	}

	rows := make([]AdminGroupMessageRow, 0, size)
	if err := q.Order("m.id DESC").Limit(size + 1).Find(&rows).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > size
	if hasMore {
		rows = rows[:size]
	}
	return rows, hasMore, nil
}
