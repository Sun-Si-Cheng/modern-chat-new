import { http } from './client'
import type {
  AuthPayload,
  Conversation,
  ConversationMember,
  FriendItem,
  FriendRequestItem,
  Message,
  Plugin,
  User,
} from '@/types/proto'

// ===== 认证（§6 auth） =====

export const authApi = {
  register: (body: { username: string; password: string; nickname: string }) =>
    http.post<AuthPayload>('/auth/register', body),
  login: (body: { username: string; password: string }) => http.post<AuthPayload>('/auth/login', body),
  forgot: (body: { email: string }) => http.post<{ ok: true }>('/auth/forgot', body),
  reset: (body: { token: string; newPassword: string }) =>
    http.post<{ ok: true }>('/auth/reset', { token: body.token, newPassword: body.newPassword }),
  me: () => http.get<User>('/me'),
  updateMe: (body: { nickname?: string; email?: string }) => http.patch<User>('/me', body),
}

// ===== 用户搜索 =====

export const userApi = {
  search: (kw: string) => http.get<User[]>('/users/search', { kw }),
}

// ===== 好友 =====

export const friendApi = {
  list: () => http.get<FriendItem[]>('/friends'),
  requests: () => http.get<FriendRequestItem[]>('/friends/requests'),
  apply: (toUsername: string) => http.post<{ ok: true }>('/friends/apply', { toUsername }),
  handle: (requestId: number, accept: boolean) => http.post<{ ok: true }>('/friends/handle', { requestId, accept }),
  remove: (friendId: number) => http.delete<{ ok: true }>(`/friends/${friendId}`),
}

// ===== 会话 / 消息 =====

export interface ConversationListResp {
  conversations: Conversation[]
}

export interface MessagesPage {
  messages: Message[]
  /** 是否还有更早消息 */
  hasMore: boolean
}

export const convApi = {
  list: () => http.get<Conversation[]>('/conversations'),
  searchGroups: (kw: string) => http.get<Conversation[]>('/conversations/search', { kw }),
  createSingle: (toUserId: number) => http.post<Conversation>('/conversations/single', { toUserId }),
  createGroup: (name: string) => http.post<Conversation>('/conversations/group', { name }),
  messages: (convId: number, beforeId?: number, limit = 30) =>
    http.get<MessagesPage>(`/conversations/${convId}/messages`, { beforeId, limit }),
  members: (convId: number) => http.get<ConversationMember[]>(`/conversations/${convId}/members`),
  join: (convId: number) => http.post<{ ok: true }>(`/conversations/${convId}/join`),
  leave: (convId: number) => http.post<{ ok: true }>(`/conversations/${convId}/leave`),
  read: (convId: number, upToId: number) => http.post<{ ok: true }>(`/conversations/${convId}/read`, { upToId }),
  rename: (convId: number, name: string) => http.patch<{ ok: true }>(`/conversations/${convId}`, { name }),
  kick: (convId: number, userId: number) => http.post<{ ok: true }>(`/conversations/${convId}/kick`, { userId }),
  transfer: (convId: number, userId: number) =>
    http.post<{ ok: true }>(`/conversations/${convId}/transfer`, { userId }),
}

// ===== 插件 =====

export const pluginApi = {
  list: () => http.get<Plugin[]>('/plugins'),
  active: () => http.get<Plugin[]>('/plugins/active'),
  /** 下发插件 JS 代码（文本） */
  code: async (id: number): Promise<string> => {
    const token = localStorage.getItem('mc_token') ?? ''
    const res = await fetch(`/api/plugins/${id}/code`, { headers: { Authorization: `Bearer ${token}` } })
    if (!res.ok) return ''
    return res.text()
  },
  upload: (file: File) => {
    const token = localStorage.getItem('mc_token') ?? ''
    return fetch('/api/plugins/upload', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/octet-stream' },
      body: file,
    }).then(async (res) => {
      const data = (await res.json().catch(() => ({}))) as Record<string, unknown>
      if (!res.ok) throw new Error(typeof data.error === 'string' ? data.error : '上传失败')
      return data as unknown as Plugin
    })
  },
  fromUrl: (url: string) => http.post<Plugin>('/plugins/from-url', { url }),
  setEnabled: (id: number, enabled: boolean) => http.patch<Plugin>(`/plugins/${id}`, { enabled }),
  remove: (id: number) => http.delete<{ ok: true }>(`/plugins/${id}`),
}

// ===== 站长后台 =====

export interface AdminUserRow extends User {
  createdAt: string
}

export interface AdminUsersPage {
  users: AdminUserRow[]
  total: number
  page: number
  pageSize: number
}

export const adminApi = {
  users: (page = 1, pageSize = 20) => http.get<AdminUsersPage>('/admin/users', { page, pageSize }),
  ban: (id: number, days: number, reason: string) =>
    http.post<{ ok: true }>(`/admin/users/${id}/ban`, { days, reason }),
  unban: (id: number) => http.post<{ ok: true }>(`/admin/users/${id}/unban`),
  resetPassword: (id: number) =>
    http.post<{ newPassword: string }>(`/admin/users/${id}/reset-password`),
}
