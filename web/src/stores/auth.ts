import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { ApiError, onUnauthorized } from '@/api/client'

import { authApi } from '@/api'
import { getToken, setToken } from '@/api/client'
import type { User } from '@/types/proto'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(getToken())
  const user = ref<User | null>(null)

  /** 会话结束回调：WS 断开、插件宿主停止等副作用由使用方注册，避免 store 之间循环依赖 */
  const sessionEndHandlers = new Set<() => void>()

  function onSessionEnd(fn: () => void) {
    sessionEndHandlers.add(fn)
    return () => sessionEndHandlers.delete(fn)
  }

  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  function applyAuth(payload: { token: string; user: User }) {
    token.value = payload.token
    setToken(payload.token)
    user.value = payload.user
  }

  async function login(username: string, password: string) {
    applyAuth(await authApi.login({ username, password }))
  }

  async function register(
    username: string,
    password: string,
    nickname: string,
    inviteCode?: string,
    email?: string,
    emailCode?: string,
  ) {
    applyAuth(await authApi.register({ username, password, nickname, inviteCode, email, emailCode }))
  }

  /** 发送注册邮箱验证码；返回服务端给的冷却秒数（冷却期内重复调用也成功） */
  async function sendRegisterCode(email: string): Promise<number> {
    const r = await authApi.emailCode(email)
    return r.cooldownSeconds ?? 60
  }

  /** 换绑邮箱：向新邮箱发验证码 */
  async function sendEmailChangeCode(email: string): Promise<number> {
    const r = await authApi.emailChangeCode(email)
    return r.cooldownSeconds ?? 60
  }

  async function fetchMe() {
    if (!token.value) return
    try {
      user.value = await authApi.me()
    } catch (e) {
      // 只有鉴权真的失效（401）才清登录态：超时、离线、502 时保留 token，
      // 否则一次网络抖动就把用户踢下线（表现为「打开设置页突然显示未登录」）
      if (e instanceof ApiError && e.status === 401) logout()
      else throw e
    }
  }

  async function updateMe(body: { nickname?: string; email?: string; avatar?: string }) {
    user.value = await authApi.updateMe(body)
  }

  function logout() {
    // mc_auth 是 HttpOnly cookie，JS 无法删除；请后端下发过期 Set-Cookie。
    // 不这样做的话，退出登录后同一浏览器在 cookie 有效期内仍能直接访问 /uploads。
    // keepalive 保证页面跳转 / 刷新过程中请求也能发出，失败不影响本地登出。
    void fetch('/api/auth/logout', { method: 'POST', keepalive: true }).catch(() => undefined)
    token.value = ''
    user.value = null
    setToken('')
    // 让 WS、插件宿主等订阅方随登录态一起收尾，避免「已登出但连接还在收消息」
    for (const fn of sessionEndHandlers) fn()
  }

  // 401 由 api/client 统一捕获：同步清掉内存态，否则 store 里仍是「已登录」
  onUnauthorized(() => {
    logout()
  })

  return {
    token,
    user,
    isLoggedIn,
    isAdmin,
    login,
    register,
    sendRegisterCode,
    sendEmailChangeCode,
    fetchMe,
    updateMe,
    logout,
    onSessionEnd,
  }
})
