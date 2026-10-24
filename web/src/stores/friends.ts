import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { friendApi } from '@/api'
import type { FriendItem, FriendRequestItem } from '@/types/proto'

export const useFriendsStore = defineStore('friends', () => {
  const friends = ref<FriendItem[]>([])
  const requests = ref<FriendRequestItem[]>([])

  const accepted = computed(() => friends.value.filter((f) => f.status === 1))
  const requestCount = computed(() => requests.value.length)

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
