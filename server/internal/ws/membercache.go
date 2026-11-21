package ws

import (
	"errors"
	"time"
)

// ---- 会话成员列表缓存 ----
//
// typing / read / recall / edit / reaction / 消息 fan-out 每帧都要
// MemberUserIDs，群越大单查越重，而成员列表只在成员增减时变化。
// 所有变更入口（REST 邀请 / 踢出 / 退群 / 公告补员）都经
// Notifier.InvalidateMembers 即时失效，短 TTL 只是兜底未接线的变更路径
// （如迁移脚本、后台运维操作直改库）。消息 fan-out 同样走缓存：失效后
// 下一次调用即恢复直查，不存在长期漏投 / 多投窗口。

const membersCacheTTL = 30 * time.Second

type membersEntry struct {
	ids []uint
	at  time.Time
}

// MemberUserIDsCached 会话成员 id 列表（短 TTL 缓存）。
// 返回值是共享的内部切片，调用方只读，不得修改或追加。
func (h *Hub) MemberUserIDsCached(convID uint) ([]uint, error) {
	if h.deps == nil || h.deps.Convs == nil {
		return nil, errors.New("ws: deps not ready")
	}
	now := time.Now()
	if v, ok := h.membersCache.Load(convID); ok {
		e := v.(*membersEntry)
		if now.Sub(e.at) < membersCacheTTL {
			return e.ids, nil
		}
	}
	ids, err := h.deps.Convs.MemberUserIDs(convID)
	if err != nil {
		return nil, err
	}
	h.membersCache.Store(convID, &membersEntry{ids: ids, at: now})
	return ids, nil
}

// InvalidateMembers 成员增减后失效缓存（Notifier 接口，REST 变更入口调用）。
func (h *Hub) InvalidateMembers(convID uint) {
	h.membersCache.Delete(convID)
}
