import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { ApiError } from '@/api/client'

import { authApi } from '@/api'
import { getToken, setToken } from '@/api/client'
import type { User } from '@/types/proto'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(getToken())
  const user = ref<User | null>(null)

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
    token.value = ''
    user.value = null
    setToken('')
  }

  return {
    token,
    user,
    isLoggedIn,
    isAdmin,
    login,
    register,
    sendRegisterCode,
    fetchMe,
    updateMe,
    logout,
  }
})
