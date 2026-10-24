package model

import "time"

// Conversation 统一会话：type=1 单聊、type=2 群聊（同构）。
type Conversation struct {
	ID        uint     `gorm:"primaryKey" json:"id"`
	Type      int8     `gorm:"not null" json:"type"`
	Name      *string  `gorm:"size:30" json:"name,omitempty"`
	OwnerID   *uint    `json:"ownerId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ConversationMember 会话成员；单聊双方均 member。
// (conversation_id, user_id) 唯一。
type ConversationMember struct {
	ID                uint      `gorm:"primaryKey" json:"-"`
	ConversationID    uint      `gorm:"uniqueIndex:idx_conv_user,priority:1" json:"-"`
	UserID            uint      `gorm:"uniqueIndex:idx_conv_user,priority:2;index" json:"userId"`
	Role              string    `gorm:"size:10;default:member" json:"role"` // owner | member
	LastReadMessageID uint64    `gorm:"default:0" json:"-"`
	JoinedAt          time.Time `gorm:"autoCreateTime" json:"joinedAt"`
}

// Message 消息。InnoDB 二级索引叶子天然含主键，
// ConversationID 的普通索引即等效 (conversation_id, id) 复合，支撑游标分页。
// (conversation_id, client_msg_id) 唯一：服务端幂等键。
type Message struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ConversationID uint      `gorm:"index;uniqueIndex:idx_conv_clientmsg,priority:1" json:"conversationId"`
	SenderID       uint      `json:"senderId"`
	ClientMsgID    string    `gorm:"size:36;uniqueIndex:idx_conv_clientmsg,priority:2" json:"clientMsgId"`
	MsgType        string    `gorm:"size:32;default:text" json:"msgType"`
	Content        string    `gorm:"size:1000" json:"content"`
	IsRecalled     bool      `json:"isRecalled"`
	CreatedAt      time.Time `gorm:"type:datetime(3)" json:"createdAt"`
}

// UserConversation 收件箱冗余行（每用户每会话一行）：会话列表排序与未读数来源。
type UserConversation struct {
	UserID            uint       `gorm:"primaryKey;autoIncrement:false" json:"-"`
	ConversationID    uint       `gorm:"primaryKey;autoIncrement:false" json:"-"`
	LastMessageID     *uint64    `json:"-"`
	LastMessageAt     *time.Time `gorm:"type:datetime(3)" json:"-"`
	UnreadCount       int        `gorm:"default:0" json:"-"`
	LastReadMessageID uint64     `gorm:"default:0" json:"-"`
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

// PasswordReset 找回密码令牌：crypto/rand 32 字节 hex（64 字符），15 分钟有效，一次性。
type PasswordReset struct {
	ID        uint      `gorm:"primaryKey" json:"-"`
	UserID    uint      `json:"-"`
	Token     string    `gorm:"size:64;uniqueIndex" json:"-"`
	ExpiresAt time.Time `json:"-"`
	Used      bool      `json:"-"`
	CreatedAt time.Time `json:"-"`
}
