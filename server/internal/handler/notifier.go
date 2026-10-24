package handler

import "github.com/gin-gonic/gin"

// Notifier REST 侧向 WS Hub 推帧的窄接口（由 ws.Hub 实现）。
type Notifier interface {
	// ToUsers 向一组在线用户的全部连接发送任意帧（JSON 序列化）。
	ToUsers(userIDs []uint, frame any)
}

// NoopNotifier 空实现（测试 / Hub 未就绪）。
type NoopNotifier struct{}

func (NoopNotifier) ToUsers([]uint, any) {}

// notifyFor 从 gin 上下文取 Notifier。
func notifyFor(c *gin.Context) Notifier {
	n, ok := c.Get("notifier")
	if !ok {
		return NoopNotifier{}
	}
	return n.(Notifier)
}
