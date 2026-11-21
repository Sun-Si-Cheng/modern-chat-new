package ws

import "gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"

// notifyPresence 上下线广播：好友 + 所有同会话成员（含本人，便于多端同步）。
//
// 修复缺陷（F1）：上线（Register 后）与下线（readPump 退出后）广播是两个独立
// goroutine，各自先查库再入队、彼此无同步——刷新页面重连时，旧连接排定的
// offline 广播可能落地在新连接的 online 之后，好友端按「online→offline」收尾，
// 把实际在线的用户显示成离线，且要等其下次上下线事件才能自愈。
// 现按用户分片互斥串行化：同一用户的上下线广播在锁内「先复查注册表、再入队」，
// 排定后在线状态已翻转的过期广播（旧连接的 offline / 秒断的 online）直接跳过，
// 由后到的广播落地最新状态，保证接收端最终收到的状态与注册表一致。
func (h *Hub) notifyPresence(u *model.User, online bool) {
	if h.deps == nil {
		return
	}
	mu := &h.presenceMu[u.ID%ShardCount]
	mu.Lock()
	defer mu.Unlock()
	if h.Online(u.ID) != online {
		return
	}
	// NewHub(nil) / 未注入依赖时由上面的 deps 判空跳过；relatedUsers 内部
	// 对各依赖字段另行判空，单测场景不会 panic。
	h.ToUsers(h.relatedUsers(u.ID), ginH{
		"type": "presence", "userId": u.ID, "online": online,
	})
}

// NotifyUserChange 昵称/头像变更广播：好友 + 同会话成员 + 本人其他设备，
// 前端据此就地更新会话列表、好友列表与群成员头像，无需刷新页面。
func (h *Hub) NotifyUserChange(userID uint, nickname, avatar, avatarColor string) {
	if h.deps == nil {
		return
	}
	h.ToUsers(h.relatedUsers(userID), ginH{
		"type":        "user_change",
		"userId":      userID,
		"nickname":    nickname,
		"avatar":      avatar,
		"avatarColor": avatarColor,
	})
}

// relatedUsers 与该用户相关的在线受众：同会话成员 ∪ 好友 ∪ 自己。
func (h *Hub) relatedUsers(uid uint) []uint {
	set := map[uint]bool{uid: true}
	// nil 判空：NewHub(&Deps{}) 等未完整注入依赖的场景（含单测）不能 panic。
	if h.deps.Convs != nil {
		if convIDs, err := h.deps.Convs.ConvIDsOfUser(uid); err == nil {
			if others, err := h.deps.Convs.MemberIDsOfConvs(convIDs); err == nil {
				for _, id := range others {
					set[id] = true
				}
			}
		}
	}
	if h.deps.Friends != nil {
		if friendIDs, err := h.deps.Friends.FriendIDs(uid); err == nil {
			for _, id := range friendIDs {
				set[id] = true
			}
		}
	}
	out := make([]uint, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}
