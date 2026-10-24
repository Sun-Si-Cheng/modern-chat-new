import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

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

  async function register(username: string, password: string, nickname: string) {
    applyAuth(await authApi.register({ username, password, nickname }))
  }

  async function fetchMe() {
    if (!token.value) return
    try {
      user.value = await authApi.me()
    } catch {
      logout()
    }
  }

  async function updateMe(body: { nickname?: string; email?: string; avatar?: string }) {
    user.value = await authApi.updateMe(body)
  }

  /** 其他端修改资料后由 WS 推送就地更新，无需重新拉取 */
  function patchProfile(patch: Pick<User, 'nickname' | 'avatar' | 'avatarColor'>) {
    if (!user.value) return
    user.value = { ...user.value, ...patch }
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
    fetchMe,
    updateMe,
    patchProfile,
    logout,
  }
})
