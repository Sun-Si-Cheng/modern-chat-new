import { ref } from 'vue'
import { defineStore } from 'pinia'

import { favoriteApi } from '@/api'
import type { FavoriteItem } from '@/types/proto'

/** 消息收藏：收藏夹列表 + 消息是否已收藏的本地判断 */
export const useFavoritesStore = defineStore('favorites', () => {
  const items = ref<FavoriteItem[]>([])
  /** 已收藏的消息 id，供消息右键菜单快速判断 */
  const ids = ref<number[]>([])
  const loading = ref(false)
  /** 已成功拉取过一次；强制刷新时传 force 绕过 */
  const loaded = ref(false)

  async function load(force = false) {
    if (loading.value) return
    if (loaded.value && !force) return
    loading.value = true
    try {
      items.value = await favoriteApi.list()
      ids.value = items.value.map((i) => i.messageId)
      loaded.value = true
    } finally {
      loading.value = false
    }
  }

  function isFavorited(messageId: number): boolean {
    return ids.value.includes(messageId)
  }

  async function add(messageId: number) {
    await favoriteApi.add(messageId)
    if (!ids.value.includes(messageId)) ids.value = [...ids.value, messageId]
    // 收藏夹列表需要重新拉取才能包含这条
    loaded.value = false
  }

  async function remove(messageId: number) {
    await favoriteApi.remove(messageId)
    ids.value = ids.value.filter((id) => id !== messageId)
    items.value = items.value.filter((i) => i.messageId !== messageId)
  }

  function reset() {
    items.value = []
    ids.value = []
    loaded.value = false
    loading.value = false
  }

  return { items, ids, loading, load, isFavorited, add, remove, reset }
})
