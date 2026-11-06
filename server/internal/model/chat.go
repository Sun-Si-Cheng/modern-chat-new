package model

import "time"

// Conversation 统一会话：type=1 单聊、type=2 群聊（同构）。
type Conversation struct {
	ID   uint    `gorm:"primaryKey" json:"id"`
	Type int8    `gorm:"not null" json:"type"`
	Name *string `gorm:"size:30" json:"name,omitempty"`
	// Avatar 群头像地址（图片上传接口返回的 URL）；空表示使用默认头像
	Avatar string `gorm:"size:255" json:"avatar,omitempty"`
	// MuteAll 全员禁言：群主/管理员不受影响
	MuteAll   bool      `gorm:"default:false" json:"muteAll"`
	OwnerID   *uint     `json:"ownerId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// 群公告：仅群主可改，空串表示没有公告
	Announcement   *string    `gorm:"size:500" json:"announcement,omitempty"`
	AnnouncementAt *time.Time `gorm:"type:datetime(3)" json:"announcementAt,omitempty"`
	AnnouncementBy *uint      `json:"announcementBy,omitempty"`
}

// ConversationMember 会话成员；单聊双方均 member。
// (conversation_id, user_id) 唯一。
type ConversationMember struct {
	ID             uint   `gorm:"primaryKey" json:"-"`
	ConversationID uint   `gorm:"uniqueIndex:idx_conv_user,priority:1" json:"-"`
	UserID         uint   `gorm:"uniqueIndex:idx_conv_user,priority:2;index" json:"userId"`
	Role           string `gorm:"size:10;default:member" json:"role"` // owner | admin | member
	// MuteUntil 禁言截止时间（NULL=未禁言）；到期自动失效。
	MuteUntil         *time.Time `gorm:"type:datetime(3)" json:"mutedUntil"`
	LastReadMessageID uint64     `gorm:"default:0" json:"-"`
	JoinedAt          time.Time  `gorm:"autoCreateTime" json:"joinedAt"`
}

// Message 消息。InnoDB 二级索引叶子天然含主键，
// ConversationID 的普通索引即等效 (conversation_id, id) 复合，支撑游标分页。
// (conversation_id, client_msg_id) 唯一：服务端幂等键。
type Message struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	ConversationID uint `gorm:"index;uniqueIndex:idx_conv_clientmsg,priority:1" json:"conversationId"`
	// SenderID 建索引：后台用户详情要按发送者统计消息数（迁移 0021），
	// 没有索引时是百万行全表扫描
	SenderID    uint   `gorm:"index:idx_messages_sender" json:"senderId"`
	ClientMsgID string `gorm:"size:36;uniqueIndex:idx_conv_clientmsg,priority:2" json:"clientMsgId"`
	MsgType     string `gorm:"size:32;default:text" json:"msgType"`
	// Content TEXT：后台允许单条消息最长 5000 字符，VARCHAR(1000) 会报 1406 或静默截断
	Content     string `gorm:"type:text" json:"content"`
	IsRecalled  bool   `json:"isRecalled"`
	// RecalledAt 撤回时间。前端据此显示「XXX 撤回了一条消息」并在 3 分钟后把
	// 这条提示从聊天记录里移除；为 NULL 表示历史遗留的撤回消息（无时间信息）。
	RecalledAt *time.Time `gorm:"type:datetime(3)" json:"recalledAt"`
	// ReplyToID 引用的消息 id（须在同会话内）。NULL 表示普通消息。
	ReplyToID *uint64 `gorm:"index" json:"replyToId,omitempty"`
	// Mentions 被 @ 的用户 id（仅群聊使用）。JSON 序列化存 text，便于扩展。
	Mentions []uint `gorm:"type:text;serializer:json" json:"mentions,omitempty"`
	// Forwarded 是否是转发来的消息（前端在气泡上标注「转发」，转发不保留原作者）
	Forwarded bool `gorm:"default:false" json:"forwarded,omitempty"`
	// EditedAt 编辑时间：NULL 表示从未编辑；前端据此显示「已编辑」
	EditedAt *time.Time `gorm:"type:datetime(3)" json:"editedAt"`
	// AdminDeletedAt 管理员删除时间：非 NULL 表示该消息对所有成员不可见。
	AdminDeletedAt *time.Time `gorm:"type:datetime(3)" json:"adminDeletedAt,omitempty"`
	// AdminDeletedBy 执行管理员删除的用户 id。
	AdminDeletedBy *uint `json:"adminDeletedBy,omitempty"`
	// ReplyTo 引用消息的摘要，不落库：查询与广播时按需装配。
	ReplyTo *ReplyBrief `gorm:"-" json:"replyTo,omitempty"`
	// Reactions 表情回应聚合，不落库：历史列表与页面加载时按需装配。
	Reactions []ReactionView `gorm:"-" json:"reactions,omitempty"`
	CreatedAt time.Time      `gorm:"type:datetime(3)" json:"createdAt"`
}

// ReactionView 单条消息上某个表情的聚合结果。
type ReactionView struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Mine  bool   `json:"mine"`
}

// MessageReaction 消息表情回应：每人对每条消息最多一个表情。
type MessageReaction struct {
	MessageID uint      `gorm:"primaryKey;autoIncrement:false" json:"-"`
	UserID    uint      `gorm:"primaryKey;autoIncrement:false;index" json:"-"`
	Emoji     string    `gorm:"size:32;not null" json:"emoji"`
	CreatedAt time.Time `gorm:"type:datetime(3)" json:"-"`
}

// ReplyBrief 引用消息的摘要快照（只渲染用，不回表取全文）。
type ReplyBrief struct {
	ID             uint64 `json:"id"`
	SenderID       uint   `json:"senderId"`
	SenderNickname string `json:"senderNickname"`
	MsgType        string `json:"msgType"`
	Content        string `json:"content"`
	IsRecalled     bool   `json:"isRecalled"`
}

// UserConversation 收件箱冗余行（每用户每会话一行）：会话列表排序与未读数来源。
type UserConversation struct {
	UserID            uint       `gorm:"primaryKey;autoIncrement:false" json:"-"`
	ConversationID    uint       `gorm:"primaryKey;autoIncrement:false" json:"-"`
	LastMessageID     *uint64    `json:"-"`
	LastMessageAt     *time.Time `gorm:"type:datetime(3)" json:"-"`
	UnreadCount       int        `gorm:"default:0" json:"-"`
	LastReadMessageID uint64     `gorm:"default:0" json:"-"`
	// Pinned 置顶：会话列表置顶，排序优先于时间。
	Pinned bool `gorm:"default:false" json:"-"`
	// Muted 免打扰：只压红点数字（显示为小圆点），未读数照常累加。
	Muted bool `gorm:"default:false" json:"-"`
	// Archived 归档：从默认会话列表收起，可在「归档」筛选里查看。
	Archived bool `gorm:"default:false" json:"-"`
	// ClearedBeforeID 清空记录水位：早于该 id 的消息不再下发给该用户。
	ClearedBeforeID uint64 `gorm:"default:0" json:"-"`
	// MentionUnread 有未读的 @我消息：即使免打扰也要提示「有人@我」。
	MentionUnread bool `gorm:"default:false" json:"-"`
}

// MessageDeletion 「仅自己删除」标记：只从该用户的聊天记录中移除，消息本体保留，
// 对方与其它成员仍可见。主键 (user_id, message_id) 保证幂等。
type MessageDeletion struct {
	UserID    uint      `gorm:"primaryKey;autoIncrement:false" json:"-"`
	MessageID uint      `gorm:"primaryKey;autoIncrement:false;index" json:"-"`
	CreatedAt time.Time `gorm:"type:datetime(3)" json:"-"`
}

// MessageFavorite 消息收藏（每用户独立）。主键 (user_id, message_id) 保证幂等。
type MessageFavorite struct {
	UserID    uint      `gorm:"primaryKey;autoIncrement:false" json:"-"`
	MessageID uint      `gorm:"primaryKey;autoIncrement:false;index" json:"-"`
	CreatedAt time.Time `gorm:"type:datetime(3)" json:"-"`
}

// UserBlock 拉黑关系：blocker_id 拉黑 blocked_id（单向记录，双向拦截由业务层判断）。
type UserBlock struct {
	BlockerID uint      `gorm:"primaryKey;autoIncrement:false" json:"-"`
	BlockedID uint      `gorm:"primaryKey;autoIncrement:false;index" json:"-"`
	CreatedAt time.Time `gorm:"type:datetime(3)" json:"-"`
}

// MessageReport 消息举报：每人对同一条消息只能举报一次。
// status: 0=待处理 1=已处理 2=已忽略（当前只落库，后台处理留待后续）。
type MessageReport struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ReporterID uint      `gorm:"uniqueIndex:uk_reporter_message,priority:1" json:"reporterId"`
	MessageID  uint      `gorm:"uniqueIndex:uk_reporter_message,priority:2;index" json:"messageId"`
	Reason     string    `gorm:"size:200;not null" json:"reason"`
	Status     int8      `gorm:"default:0" json:"status"`
	CreatedAt  time.Time `gorm:"type:datetime(3)" json:"createdAt"`
}

// PushSubscription Web Push 订阅（每个浏览器端点一行）。
type PushSubscription struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"index;not null" json:"-"`
	Endpoint  string    `gorm:"size:500;uniqueIndex" json:"endpoint"`
	P256dh    string    `gorm:"size:255;not null" json:"p256dh"`
	Auth      string    `gorm:"size:128;not null" json:"auth"`
	CreatedAt time.Time `gorm:"type:datetime(3)" json:"createdAt"`
}

// ConversationInvite 群邀请链接。token 为 32 位 hex，撤销/过期即不可用。
type ConversationInvite struct {
	Token          string     `gorm:"primaryKey;size:32" json:"token"`
	ConversationID uint       `gorm:"index;not null" json:"conversationId"`
	CreatedBy      uint       `gorm:"not null" json:"createdBy"`
	ExpiresAt      *time.Time `gorm:"type:datetime(3)" json:"expiresAt"`
	Revoked        bool       `gorm:"default:false" json:"revoked"`
	CreatedAt      time.Time  `gorm:"type:datetime(3)" json:"createdAt"`
}

// Valid 邀请链接当前是否可用。
func (c *ConversationInvite) Valid() bool {
	if c.Revoked {
		return false
	}
	return c.ExpiresAt == nil || c.ExpiresAt.After(time.Now())
}

// Friend 好友关系：user_id 视角指向 friend_id。
// status: 0=待申请（user 申请加 friend）1=已通过 2=已拒绝。
type Friend struct {
	ID        uint      `gorm:"primaryKey" json:"-"`
	UserID    uint      `gorm:"index:idx_uf,priority:1" json:"-"`
	FriendID  uint      `gorm:"index:idx_uf,priority:2" json:"-"`
	Status    int8      `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}

// ConversationFile 群文件：文件本体在 Storage，这里只记归属与展示信息。
//
// 保留 Name 与 Size 快照：Storage 里的随机文件名对人没有意义，
// 用户需要看到"我上传时叫这个名字、多大"。
type ConversationFile struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ConversationID uint      `gorm:"index;not null" json:"conversationId"`
	UploaderID     uint      `gorm:"index;not null" json:"uploaderId"`
	UploaderName   string    `gorm:"-" json:"uploaderName,omitempty"`
	Name           string    `gorm:"size:120;not null" json:"name"`
	URL            string    `gorm:"size:255;not null" json:"url"`
	Size           int64     `json:"size"`
	CreatedAt      time.Time `gorm:"type:datetime(3)" json:"createdAt"`
}

// LoginAttempt 按账号记录登录失败次数（防暴力破解）。
//
// 按 username 而不是 uid：用户不存在时也计数，避免攻击者靠「用户名不存在」
// 这一分支绕过锁定（同时也不能因此暴露账号是否存在，错误文案始终一致）。
type LoginAttempt struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Username    string     `gorm:"size:64;uniqueIndex;not null" json:"username"`
	FailedCount int        `gorm:"not null;default:0" json:"failedCount"`
	LockedUntil *time.Time `gorm:"type:datetime(3)" json:"lockedUntil"`
	UpdatedAt   time.Time  `gorm:"type:datetime(3)" json:"updatedAt"`
}

// InviteCode 注册邀请码（站长在后台生成）。
//
// 核销用条件 UPDATE 原子完成（uses < max_uses），不用「先查再改」——
// 否则并发注册会让最后一个名额被多人同时使用。
type InviteCode struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	Code      string     `gorm:"size:32;uniqueIndex;not null" json:"code"`
	CreatedBy uint       `gorm:"index;not null" json:"createdBy"`
	Creator   string     `gorm:"-" json:"creator,omitempty"`
	MaxUses   int        `gorm:"not null;default:1" json:"maxUses"`
	Uses      int        `gorm:"not null;default:0" json:"uses"`
	ExpiresAt *time.Time `gorm:"type:datetime(3)" json:"expiresAt"`
	Disabled  bool       `gorm:"default:false" json:"disabled"`
	CreatedAt time.Time  `gorm:"type:datetime(3)" json:"createdAt"`
}

// Sticker 用户自定义表情（个人表情库）。
//
// 只存 URL：文件已经在 Storage 里（本地或对象存储），这里只记归属关系。
// 与消息解耦——删表情不影响已发出的消息（消息里存的是 URL 快照）。
type Sticker struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	UserID uint `gorm:"index;not null;uniqueIndex:uk_stickers_user_url,priority:1" json:"-"`
	// 与版本化 SQL 保持一致：同一用户同一 URL 只能有一条（并发收藏也不会重复）。
	// url 建前缀索引，255*4 字节会超出 InnoDB 单列索引上限。
	URL       string    `gorm:"size:255;not null;uniqueIndex:uk_stickers_user_url,priority:2,length:191" json:"url"`
	CreatedAt time.Time `gorm:"type:datetime(3)" json:"createdAt"`
}

// PasswordReset 找回密码令牌：crypto/rand 32 字节 hex（64 字符），15 分钟有效，一次性。
type PasswordReset struct {
	ID        uint      `gorm:"primaryKey" json:"-"`
	UserID    uint      `json:"-"`
	Token     string    `gorm:"size:64;uniqueIndex" json:"-"`
	ExpiresAt time.Time `json:"-"`
	Used      bool      `json:"-"`
	CreatedAt time.Time `json:"-"`
}

// EmailCode 注册邮箱验证码：6 位数字，10 分钟有效，一次性。
// 库里只存哈希（sha256(code + email + purpose + 服务端密钥)），不存明文。
type EmailCode struct {
	ID        uint       `gorm:"primaryKey" json:"-"`
	Email     string     `gorm:"size:254;not null;index" json:"-"`
	CodeHash  string     `gorm:"size:64;not null" json:"-"`
	Purpose   string     `gorm:"size:16;not null;default:register" json:"-"`
	Attempts  int        `gorm:"not null;default:0" json:"-"`
	ExpiresAt time.Time  `gorm:"type:datetime(3);not null" json:"-"`
	UsedAt    *time.Time `gorm:"type:datetime(3)" json:"-"`
	CreatedAt time.Time  `gorm:"type:datetime(3)" json:"-"`
}

// TableName 显式指定表名（迁移里建的是 email_codes）。
func (EmailCode) TableName() string { return "email_codes" }
