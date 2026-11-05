package model

import (
	"encoding/json"
	"time"
)

// UserBrief 对外用户摘要（搜索、成员、peer 等，不含 email）。
type UserBrief struct {
	ID          uint   `json:"id"`
	Username    string `json:"username"`
	Nickname    string `json:"nickname"`
	AvatarColor string `json:"avatarColor"`
	Avatar      string `json:"avatar,omitempty"`
}

// Brief 从 User 提取摘要。
func (u *User) Brief() UserBrief {
	return UserBrief{
		ID: u.ID, Username: u.Username, Nickname: u.Nickname,
		AvatarColor: u.AvatarColor, Avatar: u.Avatar,
	}
}

// ConversationView 会话列表 / 创建会话的响应体（§3.1 Conversation）。
type ConversationView struct {
	ID                 uint       `json:"id"`
	Type               int8       `json:"type"`
	Name               *string    `json:"name"`
	Peer               *UserBrief `json:"peer"`
	OwnerID            *uint      `json:"ownerId"`
	MemberCount        int        `json:"memberCount"`
	LastMessageID      *uint64    `json:"lastMessageId"`
	LastMessageAt      *time.Time `json:"lastMessageAt"`
	LastMessagePreview *string    `json:"lastMessagePreview"`
	// LastMessageType 最后一条消息的类型：前端据此把图片/语音的 URL 预览换成占位文案
	LastMessageType *string `json:"lastMessageType"`
	UnreadCount     int     `json:"unreadCount"`
	// 会话级个人设置（存在 user_conversations，不影响其他成员）
	Pinned bool `json:"pinned"`
	Muted  bool `json:"muted"`
	// MentionUnread 有未读的 @我 消息：即使免打扰也要显示「有人@我」
	MentionUnread bool `json:"mentionUnread"`
	// 群公告（仅群聊）；没有公告时为空串
	Announcement   string     `json:"announcement"`
	AnnouncementAt *time.Time `json:"announcementAt"`
	AnnouncementBy string     `json:"announcementBy"`
}

// FavoriteView 收藏列表行（GET /api/favorites）。
type FavoriteView struct {
	MessageID      uint64    `json:"messageId"`
	ConversationID uint      `json:"conversationId"`
	ConvType       int8      `json:"convType"`
	ConvName       string    `json:"convName"`
	SenderID       uint      `json:"senderId"`
	SenderNickname string    `json:"senderNickname"`
	MsgType        string    `json:"msgType"`
	Content        string    `json:"content"`
	IsRecalled     bool      `json:"isRecalled"`
	CreatedAt      time.Time `json:"createdAt"`
	FavoritedAt    time.Time `json:"favoritedAt"`
}

// SearchHitView 消息搜索结果（GET /api/search/messages）。
type SearchHitView struct {
	MessageID      uint64    `json:"messageId"`
	ConversationID uint      `json:"conversationId"`
	ConvType       int8      `json:"convType"`
	ConvName       string    `json:"convName"`
	SenderID       uint      `json:"senderId"`
	SenderNickname string    `json:"senderNickname"`
	MsgType        string    `json:"msgType"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"createdAt"`
}

// FriendItemView 好友列表行（GET /api/friends）。
type FriendItemView struct {
	ID        uint      `json:"id"` // 关系记录 id
	Friend    UserBrief `json:"friend"`
	Status    int8      `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// FriendRequestView 收到的好友申请行（GET /api/friends/requests）。
type FriendRequestView struct {
	ID        uint      `json:"id"` // 申请记录 id
	From      UserBrief `json:"from"`
	CreatedAt time.Time `json:"createdAt"`
}

// MemberView 会话成员行（GET /api/conversations/:id/members）。
type MemberView struct {
	UserID     uint       `json:"userId"`
	Role       string     `json:"role"` // owner | admin | member
	JoinedAt   time.Time  `json:"joinedAt"`
	MutedUntil *time.Time `json:"mutedUntil"`
	User       UserBrief  `json:"user"`
}

// PluginView 插件响应体（permissions/allowedHosts 为数组）。
type PluginView struct {
	ID             uint      `json:"id"`
	Name           string    `json:"name"`
	Version        string    `json:"version"`
	Description    string    `json:"description"`
	Permissions    []string  `json:"permissions"`
	AllowedHosts   []string  `json:"allowedHosts"`
	Enabled        bool      `json:"enabled"`
	GlobalDisabled bool      `json:"globalDisabled"`
	InstalledBy    uint      `json:"installedBy"`
	CreatedAt      time.Time `json:"createdAt"`
}

// View 从 Plugin 组装响应体；JSON 字段解析失败时给空数组。
func (p *Plugin) View() PluginView {
	v := PluginView{
		ID: p.ID, Name: p.Name, Version: p.Version, Description: p.Description,
		Enabled: p.Enabled, GlobalDisabled: p.GlobalDisabled, InstalledBy: p.InstalledBy,
		CreatedAt:    p.CreatedAt,
		Permissions:  []string{},
		AllowedHosts: []string{},
	}
	_ = json.Unmarshal([]byte(p.Permissions), &v.Permissions)
	_ = json.Unmarshal([]byte(p.AllowedHosts), &v.AllowedHosts)
	return v
}
