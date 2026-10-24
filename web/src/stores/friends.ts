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

  function reset() {
    friends.value = []
    requests.value = []
  }

  return { friends, requests, accepted, requestCount, loadAll, loadRequests, apply, handle, remove, reset }
})
