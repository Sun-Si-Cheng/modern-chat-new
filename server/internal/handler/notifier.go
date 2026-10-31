package handler

import "github.com/gin-gonic/gin"

// Notifier REST 侧向 WS Hub 推帧的窄接口（由 ws.Hub 实现）。
type Notifier interface {
	// ToUsers 向一组在线用户的全部连接发送任意帧（JSON 序列化）。
	ToUsers(userIDs []uint, frame any)
	// NotifyUserChange 资料变更广播给好友与同会话成员（含本人多端）。
	NotifyUserChange(userID uint, nickname, avatar, avatarColor string)
	// KickUser 强制断开该用户的全部既有连接（封禁 / 重置密码后使用）。
	// 修复缺陷：仅推 kicked 帧不会断开连接，客户端忽略该帧即可继续收发。
	KickUser(userID uint, reason string)
	// OnlineCount 当前在线用户数（后台概览用）。
	OnlineCount() int
	// Online 该用户是否在线。
	Online(userID uint) bool
}

// NoopNotifier 空实现（测试 / Hub 未就绪）。
type NoopNotifier struct{}

func (NoopNotifier) ToUsers([]uint, any) {}

func (NoopNotifier) NotifyUserChange(uint, string, string, string) {}

func (NoopNotifier) KickUser(uint, string) {}

func (NoopNotifier) OnlineCount() int { return 0 }

func (NoopNotifier) Online(uint) bool { return false }

// notifyFor 从 gin 上下文取 Notifier。
func notifyFor(c *gin.Context) Notifier {
	n, ok := c.Get("notifier")
	if !ok {
		return NoopNotifier{}
	}
	return n.(Notifier)
}
