import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { friendApi } from '@/api'
import type { FriendItem, FriendRequestItem } from '@/types/proto'

export const useFriendsStore = defineStore('friends', () => {
  const friends = ref<FriendItem[]>([])
  const requests = ref<FriendRequestItem[]>([])

  /** 我的好友：按对方 id 去重（后端已去重，这里兜底防脏数据导致同一人出现两次） */
  const accepted = computed(() => {
    const seen = new Set<number>()
    const out: FriendItem[] = []
    for (const f of friends.value) {
      if (f.status !== 1 || seen.has(f.friend.id)) continue
      seen.add(f.friend.id)
      out.push(f)
    }
    return out
  })

  /**
   * 待处理申请：排除已经是好友的人，并按申请人去重。
   * 后端已做排除，这里再兜一层 —— 否则一条历史残留的 status=0 记录
   * 会让同一个人同时出现在「收到的好友申请」和「我的好友」两段。
   */
  const pendingRequests = computed(() => {
    const friendIds = new Set(accepted.value.map((f) => f.friend.id))
    const seen = new Set<number>()
    const out: FriendRequestItem[] = []
    for (const r of requests.value) {
      if (friendIds.has(r.from.id) || seen.has(r.from.id)) continue
      seen.add(r.from.id)
      out.push(r)
    }
    return out
  })

  const requestCount = computed(() => pendingRequests.value.length)

  async function loadAll() {
    const [f, r] = await Promise.all([friendApi.list(), friendApi.requests()])
    friends.value = f
    requests.value = r
  }

  async function loadRequests() {
    requests.value = await friendApi.requests()
  }

  async function apply(toUsername: string) {
    await friendApi.apply(toUsername)
    await loadAll()
  }

  async function handle(requestId: number, accept: boolean) {
    await friendApi.handle(requestId, accept)
    await loadAll()
  }

  async function remove(friendId: number) {
    await friendApi.remove(friendId)
    friends.value = friends.value.filter((f) => f.friend.id !== friendId)
  }

  /** 用户资料变更（昵称/头像）后就地更新好友与申请列表 */
  function applyUserProfile(u: { userId: number; nickname: string; avatar: string; avatarColor: string }) {
    for (const f of friends.value) {
      if (f.friend.id === u.userId) {
        f.friend = { ...f.friend, nickname: u.nickname, avatar: u.avatar, avatarColor: u.avatarColor }
      }
    }
    for (const r of requests.value) {
      if (r.from.id === u.userId) {
        r.from = { ...r.from, nickname: u.nickname, avatar: u.avatar, avatarColor: u.avatarColor }
      }
    }
  }

  function reset() {
    friends.value = []
    requests.value = []
  }

  return {
    friends,
    requests,
    accepted,
    pendingRequests,
    requestCount,
    loadAll,
    loadRequests,
    apply,
    handle,
    remove,
    applyUserProfile,
    reset,
  }
})
