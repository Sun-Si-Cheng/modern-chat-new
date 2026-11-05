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
      const next = await siteApi.get()
      const prev = config.value
      // 值没变就不要整体替换 config：MessagePane 的 canRecall、输入区字数上限、
      // 上传体积提示等都订阅了 config，替换对象会让它们每 30 秒全部失效重算
      //（表现为整屏消息气泡重渲一次）。
      const changed = (Object.keys(next) as Array<keyof SiteConfig>).some((k) => next[k] !== prev[k])
      if (changed) config.value = next
      loaded.value = true
      document.title = next.name
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
