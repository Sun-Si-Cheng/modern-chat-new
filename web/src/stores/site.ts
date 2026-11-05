import { ref } from 'vue'
import { defineStore } from 'pinia'

import { siteApi, type SiteConfig } from '@/api'

/**
 * 站点公开配置（GET /api/site，免登录）。
 *
 * 站长在后台改完站点名 / 消息长度 / 撤回时限 / 上传上限后，
 * 前端不需要改代码就能跟上——登录页、输入框、右键菜单都从这里取值。
 * 拉取失败时用默认值兜底：聊天功能不因一个配置接口挂掉而不可用。
 */
export const useSiteStore = defineStore('site', () => {
  const config = ref<SiteConfig>({
    name: 'ModernChat',
    allowRegister: true,
    maxMessageLength: 1000,
    recallMinutes: 2,
    maxGroupMembers: 200,
    maxImageMB: 8,
    maxAudioMB: 5,
    maxFileMB: 20,
  })
  const loaded = ref(false)

  /** 拉一次就够：站点配置在一次会话里基本不变 */
  async function load() {
    if (loaded.value) return
    try {
      config.value = await siteApi.get()
      loaded.value = true
      document.title = config.value.name
    } catch {
      // 保持默认值
    }
  }

  return { config, loaded, load }
})
