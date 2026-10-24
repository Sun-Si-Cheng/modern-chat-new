package ws

import "gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"

// notifyPresence 上下线广播：好友 + 所有同会话成员（含本人，便于多端同步）。
func (h *Hub) notifyPresence(u *model.User, online bool) {
	if h.deps == nil {
		return
	}
	h.ToUsers(h.relatedUsers(u.ID), ginH{
		"type": "presence", "userId": u.ID, "online": online,
	})
}

// relatedUsers 与该用户相关的在线受众：同会话成员 ∪ 好友 ∪ 自己。
func (h *Hub) relatedUsers(uid uint) []uint {
	set := map[uint]bool{uid: true}
	if convIDs, err := h.deps.Convs.ConvIDsOfUser(uid); err == nil {
		if others, err := h.deps.Convs.MemberIDsOfConvs(convIDs); err == nil {
			for _, id := range others {
				set[id] = true
			}
		}
	}
	if friendIDs, err := h.deps.Friends.FriendIDs(uid); err == nil {
		for _, id := range friendIDs {
			set[id] = true
		}
	}
	out := make([]uint, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}
