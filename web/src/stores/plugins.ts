import { ref } from 'vue'
import { defineStore } from 'pinia'

import { pluginApi } from '@/api'
import type { Plugin } from '@/types/proto'

/** 用户已安装插件清单；沙箱宿主从 active 列表加载（§8） */
export const usePluginsStore = defineStore('plugins', () => {
  const plugins = ref<Plugin[]>([])

  async function load() {
    plugins.value = await pluginApi.list()
  }

  async function loadActive() {
    plugins.value = await pluginApi.active()
  }

  async function setEnabled(id: number, enabled: boolean) {
    const updated = await pluginApi.setEnabled(id, enabled)
    const idx = plugins.value.findIndex((p) => p.id === id)
    if (idx >= 0) plugins.value[idx] = updated
  }

  async function remove(id: number) {
    await pluginApi.remove(id)
    plugins.value = plugins.value.filter((p) => p.id !== id)
  }

  function reset() {
    plugins.value = []
  }

  return { plugins, load, loadActive, setEnabled, remove, reset }
})
