package service

// 旧站（chat 库）只读访问层。
//
// 设计要点：
//   - 旧站与新站是同一 MySQL 实例上的不同库，这里用独立的第二个 GORM 连接
//     （独立连接池、独立 session），只执行 SELECT / Raw，绝不写入旧库。
//   - 旧站表结构可能与契约文档不一致（列缺失/表缺失/命名差异），因此启动后
//     用 information_schema 探测实际列集合，缺列的表/功能自动降级（跳过），
//     避免在真实库上直接 500。
//   - 时间列统一用 DATE_FORMAT 转字符串再解析：这样无论旧库 DSN 是否带
//     parseTime=True 都能正常读取（driver 返回 []byte 或 time.Time 都可控）。

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modern-day-team/modern-chat-new/server/internal/store"
	"gorm.io/gorm"
)

// 旧库表名常量。
const (
	legacyTableUsers         = "users"
	legacyTableMessages      = "messages"
	legacyTableGroupMessages = "group_messages"
	legacyTableGroups        = "groups"
	legacyTableGroupMembers  = "group_members"
	legacyTableFriends       = "friends"
	legacyTableFiles         = "files"
)

// LegacyDB 旧站只读库连接（懒加载）。DSN 为空时永不连接。
type LegacyDB struct {
	dsn string

	once sync.Once
	db   *gorm.DB
	err  error

	schemaOnce sync.Once
	cols       map[string]map[string]bool
	schemaErr  error
}

// newLegacyDB 构造只读连接包装；真正建立连接发生在第一次查询。
func newLegacyDB(dsn string) *LegacyDB {
	return &LegacyDB{dsn: strings.TrimSpace(dsn)}
}

// conn 返回旧库连接（首次调用时建立，失败后缓存错误不再重试）。
func (l *LegacyDB) conn() (*gorm.DB, error) {
	l.once.Do(func() {
		if l.dsn == "" {
			l.err = ErrMigrateDisabled
			return
		}
		// 只连一次：旧库读不到就整体降级为 503，不在请求里反复重试拖垮延迟。
		l.db, l.err = store.Open(l.dsn, 1, 0)
		if l.err != nil {
			l.err = fmt.Errorf("%w: %v", ErrLegacyUnavailable, l.err)
		}
	})
	return l.db, l.err
}

// Close 关闭旧库连接（进程退出时由 MigrateService.Close 调用）。
func (l *LegacyDB) Close() error {
	if l.db == nil {
		return nil
	}
	sqlDB, err := l.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// schema 探测旧库当前 schema 下所有表与列并缓存。
func (l *LegacyDB) schema() (map[string]map[string]bool, error) {
	l.schemaOnce.Do(func() {
		db, err := l.conn()
		if err != nil {
			l.schemaErr = err
			return
		}
		var rows []struct {
			TableName  string `gorm:"column:table_name"`
			ColumnName string `gorm:"column:column_name"`
		}
		if err := db.Raw(
			"SELECT TABLE_NAME AS table_name, COLUMN_NAME AS column_name " +
				"FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()",
		).Scan(&rows).Error; err != nil {
			l.schemaErr = fmt.Errorf("探测旧库表结构失败: %w", err)
			return
		}
		m := make(map[string]map[string]bool, len(rows))
		for _, r := range rows {
			if m[r.TableName] == nil {
				m[r.TableName] = make(map[string]bool, 8)
			}
			m[r.TableName][r.ColumnName] = true
		}
		l.cols = m
	})
	if l.schemaErr != nil {
		return nil, l.schemaErr
	}
	if l.cols == nil {
		l.cols = map[string]map[string]bool{}
	}
	return l.cols, nil
}

// tableColumns 返回某张表的列集合；表不存在返回 nil, nil。
func (l *LegacyDB) tableColumns(table string) (map[string]bool, error) {
	cols, err := l.schema()
	if err != nil {
		return nil, err
	}
	return cols[table], nil
}

// legacySelect 按旧库实际列拼 SELECT 列表。
// required 中任一列缺失返回 ok=false（调用方应跳过对应功能）；
// optional 是「列名 → 表达式」，只有列存在时才加入（用于 DATE_FORMAT、别名等）。
func legacySelect(cols map[string]bool, required []string, optional map[string]string) (string, bool) {
	if cols == nil {
		return "", false
	}
	for _, c := range required {
		if !cols[c] {
			return "", false
		}
	}
	parts := make([]string, 0, len(required)+len(optional))
	for _, c := range required {
		parts = append(parts, "`"+c+"`")
	}
	for c, expr := range optional {
		if cols[c] {
			parts = append(parts, expr)
		}
	}
	return strings.Join(parts, ", "), true
}

// legacyCreatedAtExpr 旧库时间列统一转字符串，兼容 parseTime 开/关两种 DSN。
func legacyCreatedAtExpr(alias string) string {
	return "DATE_FORMAT(`created_at`, '%Y-%m-%d %H:%i:%s') AS `" + alias + "`"
}

// parseLegacyTime 解析 DATE_FORMAT 读出的时间；解析失败返回零值（调用方用当前时间兜底）。
func parseLegacyTime(s *string) time.Time {
	if s == nil {
		return time.Time{}
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return time.Time{}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04:05.000", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ===== 旧库行结构 =====

// legacyUserRow 旧站 users 行（缺失列为零值）。
type legacyUserRow struct {
	ID         int32   `gorm:"column:id"`
	Username   string  `gorm:"column:username"`
	Email      *string `gorm:"column:email"`
	Password   *string `gorm:"column:password"`
	Avatar     *string `gorm:"column:avatar"`
	CreatedAtS *string `gorm:"column:created_at_s"`
}

// legacyMessageRow 旧站 messages / group_messages 行（两者共用，缺失列为零值）。
type legacyMessageRow struct {
	ID         int64   `gorm:"column:id"`
	SenderID   int32   `gorm:"column:sender_id"`
	ReceiverID int32   `gorm:"column:receiver_id"`
	GroupID    int32   `gorm:"column:group_id"`
	Content    *string `gorm:"column:content"`
	MsgType    *string `gorm:"column:type"`
	FilePath   *string `gorm:"column:file_path"`
	FileName   *string `gorm:"column:file_name"`
	FileURL    *string `gorm:"column:file_url"`
	IsDeleted  *int8   `gorm:"column:is_deleted"`
	CreatedAtS *string `gorm:"column:created_at_s"`
}

// legacyFriendRow 旧站 friends 行。
type legacyFriendRow struct {
	UserID     int32   `gorm:"column:user_id"`
	FriendID   int32   `gorm:"column:friend_id"`
	Status     *string `gorm:"column:status"`
	CreatedAtS *string `gorm:"column:created_at_s"`
}

// legacyGroupRow 旧站 groups 行。
type legacyGroupRow struct {
	ID           int32   `gorm:"column:id"`
	Name         string  `gorm:"column:name"`
	CreatorID    *int32  `gorm:"column:creator_id"`
	OwnerID      *int32  `gorm:"column:owner_id"`
	AllUserGroup *int8   `gorm:"column:all_user_group"`
	CreatedAtS   *string `gorm:"column:created_at_s"`
}

// legacyGroupMemberRow 旧站 group_members 行。
type legacyGroupMemberRow struct {
	GroupID   int32   `gorm:"column:group_id"`
	UserID    int32   `gorm:"column:user_id"`
	Role      *string `gorm:"column:role"`
	JoinedAtS *string `gorm:"column:joined_at_s"`
}

// ===== 旧库查询 =====

// user 读取旧站单个用户；不存在返回 ErrLegacyUserNotFound。
func (l *LegacyDB) user(id int32) (*legacyUserRow, error) {
	db, err := l.conn()
	if err != nil {
		return nil, err
	}
	cols, err := l.tableColumns(legacyTableUsers)
	if err != nil {
		return nil, err
	}
	sel, ok := legacySelect(cols, []string{"id", "username"}, map[string]string{
		"email":      "`email`",
		"password":   "`password`",
		"avatar":     "`avatar`",
		"created_at": legacyCreatedAtExpr("created_at_s"),
	})
	if !ok {
		return nil, ErrLegacyUnavailable
	}
	var row legacyUserRow
	if err := db.Raw("SELECT "+sel+" FROM `users` WHERE `id` = ? LIMIT 1", id).Scan(&row).Error; err != nil {
		return nil, fmt.Errorf("读取旧站用户失败: %w", err)
	}
	if row.ID == 0 {
		return nil, ErrLegacyUserNotFound
	}
	return &row, nil
}

// usernames 批量读取旧站用户名（预览展示用）。
func (l *LegacyDB) usernames(ids []int32) (map[int32]string, error) {
	out := make(map[int32]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	db, err := l.conn()
	if err != nil {
		return nil, err
	}
	cols, err := l.tableColumns(legacyTableUsers)
	if err != nil {
		return nil, err
	}
	sel, ok := legacySelect(cols, []string{"id", "username"}, nil)
	if !ok {
		return out, nil
	}
	var rows []legacyUserRow
	if err := db.Raw("SELECT "+sel+" FROM `users` WHERE `id` IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取旧站用户名失败: %w", err)
	}
	for _, r := range rows {
		out[r.ID] = r.Username
	}
	return out, nil
}

// messageCounts 统计旧站某用户的私聊 / 群聊消息条数；缺表缺列返回 0。
func (l *LegacyDB) messageCounts(userID int32) (private, group int64, err error) {
	// 私聊
	if cols, e := l.tableColumns(legacyTableMessages); e != nil {
		return 0, 0, e
	} else if cols["sender_id"] && cols["receiver_id"] {
		db, e := l.conn()
		if e != nil {
			return 0, 0, e
		}
		if e := db.Raw("SELECT COUNT(*) FROM `messages` WHERE `sender_id` = ? OR `receiver_id` = ?", userID, userID).Scan(&private).Error; e != nil {
			return 0, 0, fmt.Errorf("统计旧站私聊消息失败: %w", e)
		}
	}
	// 群聊
	if cols, e := l.tableColumns(legacyTableGroupMessages); e != nil {
		return 0, 0, e
	} else if cols["sender_id"] {
		db, e := l.conn()
		if e != nil {
			return 0, 0, e
		}
		if e := db.Raw("SELECT COUNT(*) FROM `group_messages` WHERE `sender_id` = ?", userID).Scan(&group).Error; e != nil {
			return 0, 0, fmt.Errorf("统计旧站群聊消息失败: %w", e)
		}
	}
	return private, group, nil
}

// peers 旧站私聊对端 id 列表（去重）。
func (l *LegacyDB) peers(userID int32) ([]int32, error) {
	cols, err := l.tableColumns(legacyTableMessages)
	if err != nil || !cols["sender_id"] || !cols["receiver_id"] {
		return nil, err
	}
	db, err := l.conn()
	if err != nil {
		return nil, err
	}
	var ids []int32
	if err := db.Raw(
		"SELECT DISTINCT CASE WHEN `sender_id` = ? THEN `receiver_id` ELSE `sender_id` END AS peer "+
			"FROM `messages` WHERE `sender_id` = ? OR `receiver_id` = ?",
		userID, userID, userID,
	).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("读取旧站私聊对端失败: %w", err)
	}
	return ids, nil
}

// friends 旧站好友关系（只返回 accepted；没有 status 列时返回全部）。
func (l *LegacyDB) friends(userID int32) ([]legacyFriendRow, error) {
	cols, err := l.tableColumns(legacyTableFriends)
	if err != nil || cols == nil || !cols["user_id"] || !cols["friend_id"] {
		return nil, err
	}
	db, err := l.conn()
	if err != nil {
		return nil, err
	}
	sel, _ := legacySelect(cols, []string{"user_id", "friend_id"}, map[string]string{
		"status":     "`status`",
		"created_at": legacyCreatedAtExpr("created_at_s"),
	})
	sql := "SELECT " + sel + " FROM `friends` WHERE `user_id` = ? OR `friend_id` = ?"
	if cols["status"] {
		sql += " AND `status` = 'accepted'"
	}
	var rows []legacyFriendRow
	if err := db.Raw(sql, userID, userID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取旧站好友失败: %w", err)
	}
	return rows, nil
}

// groupIDs 旧站某用户相关的群 id（群成员 + 群主/创建者），去重排序。
func (l *LegacyDB) groupIDs(userID int32) ([]int32, error) {
	seen := make(map[int32]bool)
	// 群成员关系
	if cols, err := l.tableColumns(legacyTableGroupMembers); err != nil {
		return nil, err
	} else if cols != nil && cols["group_id"] && cols["user_id"] {
		db, err := l.conn()
		if err != nil {
			return nil, err
		}
		var ids []int32
		if err := db.Raw("SELECT `group_id` FROM `group_members` WHERE `user_id` = ?", userID).Scan(&ids).Error; err != nil {
			return nil, fmt.Errorf("读取旧站群成员关系失败: %w", err)
		}
		for _, id := range ids {
			seen[id] = true
		}
	}
	// 作为群主/创建者但不在成员表里的群
	if cols, err := l.tableColumns(legacyTableGroups); err != nil {
		return nil, err
	} else if cols != nil && (cols["creator_id"] || cols["owner_id"]) {
		db, err := l.conn()
		if err != nil {
			return nil, err
		}
		conds := make([]string, 0, 2)
		args := make([]any, 0, 2)
		if cols["creator_id"] {
			conds = append(conds, "`creator_id` = ?")
			args = append(args, userID)
		}
		if cols["owner_id"] {
			conds = append(conds, "`owner_id` = ?")
			args = append(args, userID)
		}
		var ids []int32
		if err := db.Raw("SELECT `id` FROM `groups` WHERE "+strings.Join(conds, " OR "), args...).Scan(&ids).Error; err != nil {
			return nil, fmt.Errorf("读取旧站群失败: %w", err)
		}
		for _, id := range ids {
			seen[id] = true
		}
	}
	out := make([]int32, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sortInt32s(out)
	return out, nil
}

// groups 批量读取旧站群信息。
func (l *LegacyDB) groups(ids []int32) (map[int32]legacyGroupRow, error) {
	out := make(map[int32]legacyGroupRow, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	db, err := l.conn()
	if err != nil {
		return nil, err
	}
	cols, err := l.tableColumns(legacyTableGroups)
	if err != nil {
		return nil, err
	}
	sel, ok := legacySelect(cols, []string{"id", "name"}, map[string]string{
		"creator_id":     "`creator_id`",
		"owner_id":       "`owner_id`",
		"all_user_group": "`all_user_group`",
		"created_at":     legacyCreatedAtExpr("created_at_s"),
	})
	if !ok {
		return out, nil
	}
	var rows []legacyGroupRow
	if err := db.Raw("SELECT "+sel+" FROM `groups` WHERE `id` IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取旧站群信息失败: %w", err)
	}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}

// groupMembers 批量读取旧站群成员。
func (l *LegacyDB) groupMembers(groupIDs []int32) ([]legacyGroupMemberRow, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	cols, err := l.tableColumns(legacyTableGroupMembers)
	if err != nil || cols == nil || !cols["group_id"] || !cols["user_id"] {
		return nil, err
	}
	db, err := l.conn()
	if err != nil {
		return nil, err
	}
	sel, _ := legacySelect(cols, []string{"group_id", "user_id"}, map[string]string{
		"role":      "`role`",
		"joined_at": "DATE_FORMAT(`joined_at`, '%Y-%m-%d %H:%i:%s') AS `joined_at_s`",
	})
	var rows []legacyGroupMemberRow
	if err := db.Raw("SELECT "+sel+" FROM `group_members` WHERE `group_id` IN ?", groupIDs).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取旧站群成员失败: %w", err)
	}
	return rows, nil
}

// privateMessages 读取旧站私聊消息一页（按 id 升序）。
// afterID 为上一页最大 id；排除列表在调用方按 id 过滤（本层不拼巨大的 IN 条件）。
func (l *LegacyDB) privateMessages(userID int32, afterID int64, limit int) ([]legacyMessageRow, error) {
	cols, err := l.tableColumns(legacyTableMessages)
	if err != nil {
		return nil, err
	}
	if cols == nil || !cols["sender_id"] || !cols["receiver_id"] {
		return nil, nil
	}
	db, err := l.conn()
	if err != nil {
		return nil, err
	}
	sel, _ := legacySelect(cols, []string{"id", "sender_id", "receiver_id"}, map[string]string{
		"content":    "`content`",
		"type":       "`type`",
		"file_path":  "`file_path`",
		"file_name":  "`file_name`",
		"file_url":   "`file_url`",
		"is_deleted": "`is_deleted`",
		"created_at": legacyCreatedAtExpr("created_at_s"),
	})
	sql := "SELECT " + sel + " FROM `messages` WHERE (`sender_id` = ? OR `receiver_id` = ?) AND `id` > ?" +
		" ORDER BY `id` ASC LIMIT ?"
	var rows []legacyMessageRow
	if err := db.Raw(sql, userID, userID, afterID, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取旧站私聊消息失败: %w", err)
	}
	return rows, nil
}

// groupMessages 读取旧站群聊消息一页（只取该用户发送的，按 id 升序）。
// 排除列表在调用方按 id 过滤。
func (l *LegacyDB) groupMessages(userID int32, afterID int64, limit int) ([]legacyMessageRow, error) {
	cols, err := l.tableColumns(legacyTableGroupMessages)
	if err != nil {
		return nil, err
	}
	if cols == nil || !cols["sender_id"] || !cols["group_id"] {
		return nil, nil
	}
	db, err := l.conn()
	if err != nil {
		return nil, err
	}
	sel, _ := legacySelect(cols, []string{"id", "sender_id", "group_id"}, map[string]string{
		"content":    "`content`",
		"file_path":  "`file_path`",
		"file_name":  "`file_name`",
		"file_url":   "`file_url`",
		"is_deleted": "`is_deleted`",
		"created_at": legacyCreatedAtExpr("created_at_s"),
	})
	sql := "SELECT " + sel + " FROM `group_messages` WHERE `sender_id` = ? AND `id` > ?" +
		" ORDER BY `id` ASC LIMIT ?"
	var rows []legacyMessageRow
	if err := db.Raw(sql, userID, afterID, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取旧站群聊消息失败: %w", err)
	}
	return rows, nil
}

// countPrivateIn 统计排除列表里确实属于该用户私聊的旧消息条数（start 计算 total 用）。
func (l *LegacyDB) countPrivateIn(userID int32, ids []int64) (int64, error) {
	return l.countIn(legacyTableMessages, "(`sender_id` = ? OR `receiver_id` = ?)", []any{userID, userID}, ids)
}

// countGroupIn 统计排除列表里确实属于该用户发送的旧群消息条数。
func (l *LegacyDB) countGroupIn(userID int32, ids []int64) (int64, error) {
	return l.countIn(legacyTableGroupMessages, "`sender_id` = ?", []any{userID}, ids)
}

// countIn 按 id 分块（500/批）统计命中条数；缺表返回 0，避免列差异导致 500。
func (l *LegacyDB) countIn(table, where string, whereArgs []any, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	cols, err := l.tableColumns(table)
	if err != nil {
		return 0, err
	}
	if cols == nil || !cols["id"] {
		return 0, nil
	}
	db, err := l.conn()
	if err != nil {
		return 0, err
	}
	const chunk = 500
	var total int64
	sql := "SELECT COUNT(*) FROM `" + table + "` WHERE " + where + " AND `id` IN ?"
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		args := make([]any, 0, len(whereArgs)+1)
		args = append(args, whereArgs...)
		args = append(args, ids[start:end])
		var n int64
		if err := db.Raw(sql, args...).Scan(&n).Error; err != nil {
			return 0, fmt.Errorf("统计旧站排除消息失败: %w", err)
		}
		total += n
	}
	return total, nil
}

// fileCount 统计旧站用户上传文件数；缺表返回 0。
func (l *LegacyDB) fileCount(userID int32) (int64, error) {
	cols, err := l.tableColumns(legacyTableFiles)
	if err != nil || cols == nil || !cols["user_id"] {
		return 0, err
	}
	db, err := l.conn()
	if err != nil {
		return 0, err
	}
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM `files` WHERE `user_id` = ?", userID).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("统计旧站文件失败: %w", err)
	}
	return n, nil
}

// recentPrivate 读取旧站最近私聊消息（预览 sample 用）。
func (l *LegacyDB) recentPrivate(userID int32, limit int) ([]legacyMessageRow, error) {
	cols, err := l.tableColumns(legacyTableMessages)
	if err != nil || cols == nil || !cols["sender_id"] || !cols["receiver_id"] {
		return nil, err
	}
	db, err := l.conn()
	if err != nil {
		return nil, err
	}
	sel, _ := legacySelect(cols, []string{"id", "sender_id", "receiver_id"}, map[string]string{
		"content":    "`content`",
		"type":       "`type`",
		"file_path":  "`file_path`",
		"file_name":  "`file_name`",
		"is_deleted": "`is_deleted`",
		"created_at": legacyCreatedAtExpr("created_at_s"),
	})
	order := "`id` DESC"
	if cols["created_at"] {
		order = "`created_at` DESC, `id` DESC"
	}
	var rows []legacyMessageRow
	if err := db.Raw("SELECT "+sel+" FROM `messages` WHERE `sender_id` = ? OR `receiver_id` = ? ORDER BY "+order+" LIMIT ?",
		userID, userID, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取旧站私聊预览失败: %w", err)
	}
	return rows, nil
}

// recentGroup 读取旧站最近群聊消息（预览 sample 用）。
func (l *LegacyDB) recentGroup(userID int32, limit int) ([]legacyMessageRow, error) {
	cols, err := l.tableColumns(legacyTableGroupMessages)
	if err != nil || cols == nil || !cols["sender_id"] {
		return nil, err
	}
	db, err := l.conn()
	if err != nil {
		return nil, err
	}
	sel, _ := legacySelect(cols, []string{"id", "sender_id"}, map[string]string{
		"group_id":   "`group_id`",
		"content":    "`content`",
		"file_path":  "`file_path`",
		"file_name":  "`file_name`",
		"is_deleted": "`is_deleted`",
		"created_at": legacyCreatedAtExpr("created_at_s"),
	})
	order := "`id` DESC"
	if cols["created_at"] {
		order = "`created_at` DESC, `id` DESC"
	}
	var rows []legacyMessageRow
	if err := db.Raw("SELECT "+sel+" FROM `group_messages` WHERE `sender_id` = ? ORDER BY "+order+" LIMIT ?",
		userID, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取旧站群聊预览失败: %w", err)
	}
	return rows, nil
}

// ===== 小工具 =====

// int64Keys 取 map 的键（IN 查询参数）。
func int64Keys(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// sortInt32s 简单升序排序（避免引入 sort 包的重复样板）。
func sortInt32s(a []int32) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
