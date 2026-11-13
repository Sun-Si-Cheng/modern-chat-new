package model

import "time"

// 旧站迁移相关模型（契约 §5）。
// 生产建表走 migrations/0026_legacy_migration.up.sql（MIGRATE=true），
// 开发模式走 store.AutoMigrate；两边字段保持一致。

// LegacyUserMap 旧站用户 id → 新站用户 id 映射。
// 一行代表「某个旧站账号已经迁移成新站账号」，用于幂等与 complete-existing。
type LegacyUserMap struct {
	// LegacyID 旧站 users.id（主键，不自动增长）
	LegacyID int32 `gorm:"column:legacy_id;primaryKey;autoIncrement:false"`
	// UserID 新站 users.id（唯一：一个旧账号只对应一个新账号）
	UserID    uint      `gorm:"column:user_id;not null;uniqueIndex"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime(3)"`
}

// TableName 固定表名，避免 GORM 复数化规则变化。
func (LegacyUserMap) TableName() string { return "legacy_user_map" }

// 迁移阶段常量；tick 按该顺序推进（契约 §4.3）。
const (
	MigratePhaseUsers    = "users"
	MigratePhaseFriends  = "friends"
	MigratePhaseGroups   = "groups"
	MigratePhaseMessages = "messages"
	MigratePhaseInbox    = "inbox"
	MigratePhaseDone     = "done"
)

// MigrationJob 一次迁移任务（一个旧站用户一个任务）。
type MigrationJob struct {
	// ID 32 位 hex，由服务端生成
	ID string `gorm:"column:id;primaryKey;size:32"`
	// LegacyUserID 本次迁移的旧站用户 id
	LegacyUserID int32 `gorm:"column:legacy_user_id;not null;index"`
	// Phase 当前阶段：users/friends/groups/messages/inbox/done
	Phase string `gorm:"column:phase;size:16;not null"`
	// Done 已迁移的消息条数（用于百分比；账号/好友/群阶段保持 0）
	Done int `gorm:"column:done;not null;default:0"`
	// Total 本次选择迁移的消息总条数（私聊 + 群聊）
	Total int `gorm:"column:total;not null;default:0"`
	// Cursor messages 阶段游标，格式 `p:<旧私聊消息id>;g:<旧群聊消息id>;dp:0|1;dg:0|1`
	Cursor *string `gorm:"column:cursor;size:64"`
	// ExcludePrivateIDs 用户在 start 时明确排除的旧站私聊消息 id（逗号分隔，空=全部迁移）。
	// 契约 migration_jobs 建表补充这两列（见 docs §5），这样任务可持久化排除列表，
	// 进程重启后 tick 仍能按「排除列表」精确续迁。
	ExcludePrivateIDs string `gorm:"column:exclude_private_ids;type:text"`
	// ExcludeGroupIDs 用户在 start 时明确排除的旧站群聊消息 id（逗号分隔，空=全部迁移）
	ExcludeGroupIDs string `gorm:"column:exclude_group_ids;type:text"`
	// Finished 是否全部阶段完成
	Finished bool `gorm:"column:finished;not null;default:false"`
	// CreatedAt/UpdatedAt 任务创建与更新时间
	CreatedAt time.Time `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime(3)"`
}

// TableName 固定表名。
func (MigrationJob) TableName() string { return "migration_jobs" }

// MigrationSSOToken 迁移完成后签发的一次性登录票据（token + key 双因子）。
// 只存 sha256 哈希；10 分钟过期，使用后写 used_at 防重放。
type MigrationSSOToken struct {
	// TokenHash sha256(token) 的 64 位 hex
	TokenHash string `gorm:"column:token_hash;primaryKey;size:64"`
	// KeyHash sha256(key) 的 64 位 hex
	KeyHash string `gorm:"column:key_hash;size:64;not null"`
	// UserID 登录后对应的新站用户 id
	UserID uint `gorm:"column:user_id;not null;index"`
	// ExpiresAt 过期时间
	ExpiresAt time.Time `gorm:"column:expires_at;type:datetime(3);not null"`
	// UsedAt 使用时间；非空表示票据已被消费
	UsedAt *time.Time `gorm:"column:used_at;type:datetime(3)"`
}

// TableName 固定表名。
func (MigrationSSOToken) TableName() string { return "migration_sso_tokens" }
