package ws

import "gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"

// notifyPresence 上下线广播：好友 + 所有同会话成员（含本人，便于多端同步）。
func (h *Hub) notifyPresence(u *model.User, online bool) {
	if h.deps == nil {
		return
	}
	// 修复缺陷：NewHub(nil) / 未注入依赖时 deps 非 nil 但字段为 nil，直接调用会 panic。
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
