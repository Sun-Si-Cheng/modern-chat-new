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
  // 默认值一律取「最保守」：配置没拉回来之前不露出注册入口、不渲染邮箱字段。
  // 用 fail-open 默认值会让用户在站点关闭注册 / 未配邮件服务时白填一遍表单。
  const config = ref<SiteConfig>({
    name: 'ModernChat',
    allowRegister: false,
    maintenanceMode: false,
    maintenanceMessage: '站点正在维护，请稍后再试。',
    requireInviteCode: false,
    requireEmail: false,
    requireEmailVerify: false,
    maxMessageLength: 1000,
    recallMinutes: 2,
    maxGroupMembers: 200,
    maxImageMB: 8,
    maxAudioMB: 5,
    maxFileMB: 20,
    maxVideoMB: 30,
  })
  const loaded = ref(false)

  async function refresh() {
    try {
      config.value = await siteApi.get()
      loaded.value = true
      document.title = config.value.name
    } catch {
      // 保持上一次的值：拉取失败不能误判成「维护结束」把用户放进去
    }
  }

  /** 首屏拉一次 */
  async function load() {
    if (loaded.value) return
    await refresh()
  }

  return { config, loaded, load, refresh }
})
