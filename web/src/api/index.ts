import { ApiError, http } from './client'
import type {
  AuthPayload,
  Conversation,
  ConversationMember,
  FavoriteItem,
  FriendItem,
  FriendRequestItem,
  Message,
  Plugin,
  SearchHit,
  User,
} from '@/types/proto'

// ===== 认证（§6 auth） =====

export const authApi = {
  register: (body: {
    username: string
    password: string
    nickname: string
    /** 站点开启「注册需邀请码」时必填 */
    inviteCode?: string
    /** 站点开启「注册需邮箱验证」时必填 */
    email?: string
    /** 邮件里的 6 位验证码 */
    emailCode?: string
  }) => http.post<AuthPayload>('/auth/register', body),
  login: (body: { username: string; password: string }) => http.post<AuthPayload>('/auth/login', body),
  /** 发送注册验证码；返回剩余冷却秒数（冷却期内重复调用也返回 200） */
  emailCode: (email: string) =>
    http.post<{ ok: true; cooldownSeconds: number }>('/auth/email-code', { email }),
  forgot: (body: { email: string }) => http.post<{ ok: true }>('/auth/forgot', body),
  reset: (body: { token: string; newPassword: string }) =>
    http.post<{ ok: true }>('/auth/reset', { token: body.token, newPassword: body.newPassword }),
  me: () => http.get<User>('/me'),
  /** 退出所有设备：后端 token_version+1，所有旧 JWT 失效 */
  logoutOthers: () => http.post<{ ok: true }>('/me/logout-others'),
  updateMe: (body: { nickname?: string; email?: string; avatar?: string }) => http.patch<User>('/me', body),
}

// ===== 用户搜索 =====

export const userApi = {
  search: (kw: string) => http.get<User[]>('/users/search', { kw }),
  /** 查询我是否拉黑了该用户 */
  blockStatus: (userId: number) => http.get<{ blocked: boolean }>(`/users/${userId}/block`),
  block: (userId: number) => http.post<{ ok: true }>(`/users/${userId}/block`),
  unblock: (userId: number) => http.delete<{ ok: true }>(`/users/${userId}/block`),
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

/** 个人表情库：表情文件走 upload(kind=sticker)，这里只维护收藏关系 */
export const stickerApi = {
  list: () => http.get<Sticker[]>('/stickers'),
  add: (url: string) => http.post<Sticker>('/stickers', { url }),
  remove: (id: number) => http.delete<{ ok: true }>(`/stickers/${id}`),
}

export interface Sticker {
  id: number
  url: string
  createdAt: string
}

/** 群文件（文件本体在存储里，这里只是目录项） */
export interface ConversationFile {
  id: number
  conversationId: number
  uploaderId: number
  uploaderName: string
  name: string
  url: string
  size: number
  createdAt: string
}

/** 图片 / 语音上传：先传文件拿到 URL，再发消息（消息里只存 URL） */
export const mediaApi = {
  upload: (kind: 'image' | 'audio' | 'sticker' | 'file', file: Blob, filename = 'file') => {
    const form = new FormData()
    form.append('file', file, filename)
    return http.upload<{ url: string; size: number; kind: string }>(`/upload?kind=${kind}`, form)
  },
}

export const convApi = {
  list: () => http.get<Conversation[]>('/conversations'),
  searchGroups: (kw: string) => http.get<Conversation[]>('/conversations/search', { kw }),
  createSingle: (toUserId: number) => http.post<Conversation>('/conversations/single', { toUserId }),
  createGroup: (name: string) => http.post<Conversation>('/conversations/group', { name }),
  messages: (convId: number, beforeId?: number, limit = 30) =>
    http.get<MessagesPage>(`/conversations/${convId}/messages`, { beforeId, limit }),
  /** 离线/断线增量补齐：返回 afterId 之后的消息（升序） */
  messagesAfter: (convId: number, afterId: number, limit = 100) =>
    http.get<MessagesPage>(`/conversations/${convId}/messages`, { afterId, limit }),
  members: (convId: number) => http.get<ConversationMember[]>(`/conversations/${convId}/members`),
  join: (convId: number) => http.post<{ ok: true }>(`/conversations/${convId}/join`),
  /** 拉人入群：只能邀请自己的好友，已入群的人会被忽略 */
  invite: (convId: number, userIds: number[]) =>
    http.post<{ ok: true; added: number }>(`/conversations/${convId}/invite`, { userIds }),
  /** 生成群邀请链接（expiresDays=0 表示永久有效） */
  createInviteLink: (convId: number, expiresDays = 7) =>
    http.post<{ token: string; path: string; expiresAt?: string }>(
      `/conversations/${convId}/invite-link`,
      { expiresDays },
    ),
  leave: (convId: number) => http.post<{ ok: true }>(`/conversations/${convId}/leave`),
  read: (convId: number, upToId: number) => http.post<{ ok: true }>(`/conversations/${convId}/read`, { upToId }),
  rename: (convId: number, name: string) => http.patch<{ ok: true }>(`/conversations/${convId}`, { name }),
  /** 修改群头像；空串恢复默认头像 */
  setAvatar: (convId: number, avatar: string) =>
    http.put<{ ok: true }>(`/conversations/${convId}/avatar`, { avatar }),
  /** 群文件列表 / 记账 / 删除（文件本体走 upload(kind=file)） */
  files: (convId: number) => http.get<ConversationFile[]>(`/conversations/${convId}/files`),
  addFile: (convId: number, f: { name: string; url: string; size: number }) =>
    http.post<ConversationFile>(`/conversations/${convId}/files`, f),
  removeFile: (id: number) => http.delete<{ ok: true }>(`/files/${id}`),

  /** 群公告（仅群主）；空串表示撤销 */
  setAnnouncement: (convId: number, content: string) =>
    http.put<{ ok: true }>(`/conversations/${convId}/announcement`, { content }),

  /** 会话个人设置（置顶 / 免打扰）：不传的字段表示不修改 */
  setPrefs: (convId: number, prefs: { pinned?: boolean; muted?: boolean; archived?: boolean }) =>
    http.patch<{ ok: true; pinned: boolean; muted: boolean; archived: boolean }>(
      `/conversations/${convId}/prefs`,
      prefs,
    ),
  /** 从自己的会话列表移除（不退出群、不删除消息） */
  remove: (convId: number) => http.delete<{ ok: true }>(`/conversations/${convId}`),
  /** 清空我在该会话的聊天记录 */
  clear: (convId: number, upToId?: number) =>
    http.post<{ ok: true; clearedBeforeId: number }>(`/conversations/${convId}/clear`, { upToId }),
  /** 群全员禁言开关 */
  setMuteAll: (convId: number, muted: boolean) =>
    http.post<{ ok: true }>(`/conversations/${convId}/mute-all`, { muted }),
  kick: (convId: number, userId: number) => http.post<{ ok: true }>(`/conversations/${convId}/kick`, { userId }),
  /** 群主设置 / 取消管理员 */
  setAdmin: (convId: number, userId: number, admin: boolean) =>
    http.post<{ ok: true }>(`/conversations/${convId}/admin`, { userId, admin }),
  /** 禁言 / 解除禁言（minutes=0） */
  mute: (convId: number, userId: number, minutes: number) =>
    http.post<{ ok: true; mutedUntil?: string }>(`/conversations/${convId}/mute`, { userId, minutes }),
  transfer: (convId: number, userId: number) =>
    http.post<{ ok: true }>(`/conversations/${convId}/transfer`, { userId }),
  /** 仅自己删除：只从当前账号的聊天记录移除，对方仍可见 */
  deleteMessage: (convId: number, messageId: number) =>
    http.delete<{ ok: true }>(`/conversations/${convId}/messages/${messageId}`),
}

// ===== Web Push =====

export const pushApi = {
  /** 是否启用 + VAPID 公钥 */
  key: () => http.get<{ enabled: boolean; key: string }>('/push/key'),
  subscribe: (s: { endpoint: string; p256dh: string; auth: string }) =>
    http.post<{ ok: true }>('/push/subscribe', s),
  unsubscribe: (endpoint: string) =>
    http.delete<{ ok: true }>(`/push/subscribe?endpoint=${encodeURIComponent(endpoint)}`),
}

/** 群邀请链接（免登录可查群信息，加入需要登录） */
export interface InviteInfo {
  convId: number
  name: string | null
  avatar: string
  memberCount: number
}

export const inviteApi = {
  info: (token: string) => http.get<InviteInfo>(`/invite/${token}`),
  join: (token: string) =>
    http.post<{ ok: true; convId: number; alreadyMember?: boolean }>(`/invite/${token}/join`),
}

// ===== 消息搜索 =====

export const searchApi = {
  /** convId 省略时为全局搜索；只返回当前用户已加入的会话 */
  messages: (kw: string, convId?: number, limit = 30) =>
    http.get<SearchHit[]>('/search/messages', { kw, convId, limit }),
}

// ===== 收藏 =====

export const favoriteApi = {
  list: () => http.get<FavoriteItem[]>('/favorites'),
  add: (messageId: number) => http.post<{ ok: true }>(`/messages/${messageId}/favorite`),
  remove: (messageId: number) => http.delete<{ ok: true }>(`/messages/${messageId}/favorite`),
}

// ===== 治理（举报 / 管理员删除违规消息） =====

export const moderationApi = {
  /** 举报消息：同一人对同一条消息只能举报一次（重复提交返回 duplicate=true） */
  report: (messageId: number, reason: string) =>
    http.post<{ ok: true; duplicate?: boolean }>(`/messages/${messageId}/report`, { reason }),
  /** 管理员删除违规消息：站点管理员或群主/管理员可用 */
  adminDelete: (messageId: number) =>
    http.post<{ ok: true; duplicate?: boolean }>(`/messages/${messageId}/admin-delete`),
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
    // .ts / .mts 交给服务端用 esbuild 转译；其余按 JS 原样入库
    const lang = /\.m?ts$/i.test(file.name) ? 'ts' : 'js'
    return fetch(`/api/plugins/upload?lang=${lang}`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/octet-stream' },
      body: file,
    }).then(async (res) => {
      const data = (await res.json().catch(() => ({}))) as Record<string, unknown>
      if (!res.ok) throw new Error(typeof data.error === 'string' ? data.error : '上传失败')
      return { plugin: data as unknown as Plugin, updated: res.headers.get('X-Plugin-Updated') === 'true' }
    })
  },
  /** URL 安装 / 升级（同名覆盖由服务端处理，updated 由响应头标记） */
  fromUrl: (url: string) => {
    const token = localStorage.getItem('mc_token') ?? ''
    return fetch('/api/plugins/from-url', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ url }),
    }).then(async (res) => {
      const data = (await res.json().catch(() => ({}))) as Record<string, unknown>
      if (!res.ok) throw new ApiError(res.status, typeof data.error === 'string' ? data.error : '安装失败')
      return { plugin: data as unknown as Plugin, updated: res.headers.get('X-Plugin-Updated') === 'true' }
    })
  },
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

/** 后台用户列表筛选条件（空值表示不限） */
export interface AdminUserFilter {
  kw?: string
  role?: '' | 'user' | 'admin'
  status?: '' | 'active' | 'banned'
}

export const adminApi = {
  /** 概览统计 */
  stats: () => http.get<AdminStats>('/admin/stats'),
  users: (page = 1, pageSize = 20, filter?: AdminUserFilter) =>
    http.get<AdminUsersPage>('/admin/users', { page, pageSize, ...filter }),
  userDetail: (id: number) => http.get<AdminUserDetail>(`/admin/users/${id}`),
  ban: (id: number, days: number, reason: string) =>
    http.post<{ ok: true }>(`/admin/users/${id}/ban`, { days, reason }),
  unban: (id: number) => http.post<{ ok: true }>(`/admin/users/${id}/unban`),
  /** 强制下线（不封禁）：断开连接，用户可立即重新登录 */
  kick: (id: number) => http.post<{ ok: true; wasOnline: boolean }>(`/admin/users/${id}/kick`),
  resetPassword: (id: number) =>
    http.post<{ newPassword: string }>(`/admin/users/${id}/reset-password`),
  plugins: (page = 1, pageSize = 20) => http.get<AdminPluginsPage>('/admin/plugins', { page, pageSize }),
  setPluginGlobalDisabled: (name: string, disabled: boolean) =>
    http.post<{ ok: true; affected: number }>('/admin/plugins/global-disable', { name, disabled }),
  /** 按插件名强制卸载（userId=0 表示所有用户） */
  purgePlugin: (name: string, userId = 0) =>
    http.post<{ ok: true; affected: number }>('/admin/plugins/purge', { name, userId }),
  /** 调整角色：设 / 取消站长 */
  setRole: (id: number, role: 'user' | 'admin') =>
    http.post<{ ok: true; changed: boolean }>(`/admin/users/${id}/role`, { role }),
  /** 全站公告：写入系统会话并推送给所有用户 */
  announce: (content: string) =>
    http.post<{ ok: true; convId: number; messageId: number; recipients: number }>('/admin/announce', {
      content,
    }),
  audits: (page = 1, pageSize = 20) =>
    http.get<AdminAuditsPage>('/admin/audits', { page, pageSize }),

  /** 系统设置：SMTP / Redis / 对象存储（敏感项只回 hasXxx） */
  settings: () => http.get<AdminSettings>('/admin/settings'),
  saveSettings: (payload: AdminSettingsPayload) =>
    http.put<{ ok: true; redisNeedsRestart: boolean; storageApplied: boolean }>(
      '/admin/settings',
      payload,
    ),
  testSMTP: (to: string) => http.post<{ ok: true }>('/admin/settings/smtp/test', { to }),

  /** 运维：附件统计 / 孤儿文件清理 / 数据库备份 */
  storageStats: () => http.get<StorageStats>('/admin/storage/stats'),
  gcStorage: (dryRun = false) =>
    http.post<{ ok: true; dryRun: boolean; deleted: number; freedBytes: number }>(
      '/admin/storage/gc',
      { dryRun },
    ),
  backups: () => http.get<{ backups: BackupFile[]; dir: string }>('/admin/backups'),
  runBackup: () => http.post<{ ok: true; file: string }>('/admin/backups'),

  /** 注册邀请码 */
  invites: () => http.get<InviteCode[]>('/admin/invites'),
  createInvites: (body: { maxUses: number; expiresDays: number; count: number }) =>
    http.post<InviteCode[]>('/admin/invites', body),
  disableInvite: (id: number, disabled: boolean) =>
    http.post<{ ok: true }>(`/admin/invites/${id}/disable`, { disabled }),
}

/** 注册邀请码 */
export interface InviteCode {
  id: number
  code: string
  createdBy: number
  creator: string
  maxUses: number
  uses: number
  expiresAt: string | null
  disabled: boolean
  createdAt: string
}

/** 系统设置（读）：敏感字段只有 hasXxx，永不回显明文 */
export interface AdminSettings {
  smtp: { host: string; port: string; user: string; from: string; hasPass: boolean }
  redis: { addr: string; db: string; hasPass: boolean }
  storage: {
    endpoint: string
    region: string
    bucket: string
    accessKey: string
    hasSecretKey: boolean
    /** 对外访问前缀（CDN），留空按 endpoint/bucket 拼 */
    publicBase: string
  }
  /** 站点信息（改了即时生效） */
  site: SiteConfig
  /** 聊天规则（即时生效） */
  chat: ChatRules
  /** 上传体积上限（MB，即时生效） */
  upload: UploadLimits
  /** 登录安全策略 */
  security: SecurityRules
  smtpEnabled: boolean
  redisEnabled: boolean
  /** 对象存储是否已配置（配了 Endpoint + Bucket 即为 true） */
  storageApplied: boolean
  /** Redis 限流器在启动时构造，改动需重启 */
  redisNeedsRestart: boolean
}

/** 站点公开配置（GET /api/site，无需登录） */
export interface SiteConfig {
  name: string
  allowRegister: boolean
  /** 维护模式：非管理员只见维护页 */
  maintenanceMode: boolean
  maintenanceMessage: string
  /** 注册是否需要邀请码 */
  requireInviteCode: boolean
  /** 注册是否必填邮箱（开关开启即必填，首个账号也必填） */
  requireEmail: boolean
  /** 注册是否还需要邮箱验证码（开关 && SMTP 可用 && 已有账号；首个账号豁免） */
  requireEmailVerify: boolean
  maxMessageLength: number
  recallMinutes: number
  maxGroupMembers: number
  maxImageMB: number
  maxAudioMB: number
  maxFileMB: number
}

export interface ChatRules {
  maxMessageLength: number
  recallMinutes: number
  maxGroupMembers: number
  /** 敏感词（换行分隔），命中的文本消息会被服务端拒绝 */
  blockedWords: string
}

export interface UploadLimits {
  maxImageMB: number
  maxAudioMB: number
  maxFileMB: number
  /** 本地上传目录总配额（MB，0=不限制） */
  maxStorageMB: number
}

/** 运维：本地附件用量 */
export interface StorageStats {
  files: number
  bytes: number
  referenced: number
  orphans: number
  orphanBytes: number
}

/** 运维：数据库备份文件 */
export interface BackupFile {
  name: string
  size: number
  createdAt: string
}

/** 系统设置（写）：敏感字段留空表示不修改 */
export interface AdminSettingsPayload {
  smtp: { host: string; port: string; user: string; from: string; pass?: string }
  redis: { addr: string; db: string; pass?: string }
  storage: {
    endpoint: string
    region: string
    bucket: string
    accessKey: string
    secretKey?: string
    publicBase?: string
  }
  site: {
    name: string
    allowRegister: boolean
    maintenanceMode: boolean
    maintenanceMessage: string
    requireInviteCode: boolean
    requireEmailVerify: boolean
  }
  chat: ChatRules
  upload: UploadLimits
  security: SecurityRules
}

export interface SecurityRules {
  loginMaxAttempts: number
  loginLockMinutes: number
}

/** 站点公开信息（登录页也用，不需要 token） */
export const siteApi = {
  get: () => http.get<SiteConfig>('/site'),
}

/** 站长操作审计记录 */
export interface AdminAudit {
  id: number
  adminId: number
  action: string
  targetType: string
  targetId: number
  targetName: string
  detail: string
  ip: string
  createdAt: string
}

export interface AdminAuditsPage {
  audits: AdminAudit[]
  total: number
  page: number
  pageSize: number
}

/** 站长概览：用户 / 会话 / 消息 / 插件 / 在线 / 运行时长 */
export interface AdminStats {
  users: { total: number; today: number; banned: number; admins: number }
  conversations: { single: number; group: number }
  messages: { total: number; today: number }
  plugins: { installed: number; enabled: number; disabled: number }
  online: number
  uptimeSeconds: number
  goVersion: string
}

/** 站长视角：单个用户详情 */
export interface AdminUserDetail {
  user: AdminUserRow
  stats: { conversations: number; messages: number; plugins: number }
  online: boolean
}

// ===== 系统信息 =====

export interface VersionInfo {
  version: string
  commit: string
  buildTime: string
}

export const systemApi = {
  /** 当前构建版本（免鉴权，用于核对升级是否生效） */
  version: () => http.get<VersionInfo>('/version'),
}

/** 站长视角：按插件名聚合的行 */
export interface AdminPluginRow {
  name: string
  version: string
  installCount: number
  enabledCount: number
  globalDisabled: boolean
  createdAt: string
}

export interface AdminPluginsPage {
  plugins: AdminPluginRow[]
  total: number
  page: number
  pageSize: number
}
