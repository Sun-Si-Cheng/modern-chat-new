/**
 * ModernChat 前后端契约类型（WS 协议见设计文档 §7，REST 见 §6）。
 * 约定：服务端 JSON 字段一律 camelCase。
 */

// ===== 用户 =====

export interface User {
  id: number
  username: string
  nickname: string
  avatarColor: string
  /** 自定义头像 data URL；空表示用默认头像 */
  avatar?: string
  email?: string | null
  role: 'user' | 'admin'
  bannedUntil?: string | null
  banReason?: string | null
}

export interface AuthPayload {
  token: string
  user: User
}

// ===== 会话 =====

/** 1=单聊 2=群聊 3=系统公告会话 */
export type ConvType = 1 | 2 | 3

export interface Conversation {
  id: number
  type: ConvType
  /** 群名；单聊为 null，展示时取 peer 昵称 */
  name: string | null
  /** 群头像地址；单聊为空（用 peer 的资料） */
  avatar?: string
  /** 单聊对方的公开信息（服务端拼装）；群聊为 null */
  peer: Pick<User, 'id' | 'username' | 'nickname' | 'avatarColor' | 'avatar'> | null
  ownerId: number | null
  memberCount: number
  lastMessageId: number | null
  lastMessageAt: string | null
  lastMessagePreview: string | null
  /** 最后一条消息的类型：image/audio 时列表里显示占位文案而不是文件地址 */
  lastMessageType: string | null
  /** 当前用户已读游标（离线补齐与多端同步用） */
  lastReadMessageId?: number
  unreadCount: number
  /** 置顶（个人设置，不影响其他成员） */
  pinned: boolean
  /** 免打扰：红点显示为小圆点，不显示数字 */
  muted: boolean
  /** 有未读的 @我 消息：免打扰时也要提示「有人@我」 */
  mentionUnread: boolean
  /** 群公告（仅群聊），空串表示没有 */
  announcement: string
  announcementAt: string | null
  /** 公告发布者昵称 */
  announcementBy: string
}

export interface ConversationMember {
  userId: number
  role: 'owner' | 'admin' | 'member'
  joinedAt: string
  /** 禁言截止时间；null/过期表示未禁言 */
  mutedUntil?: string | null
  user: Pick<User, 'id' | 'username' | 'nickname' | 'avatarColor' | 'avatar'>
}

// ===== 消息 =====

/** 引用消息的摘要（服务端截断到 60 字，只用于渲染引用块） */
export interface ReplyBrief {
  id: number
  senderId: number
  senderNickname: string
  msgType: string
  content: string
  isRecalled: boolean
}

/** 单条消息上某个表情的聚合 */
export interface ReactionView {
  emoji: string
  count: number
  mine: boolean
}

export interface Message {
  id: number
  conversationId: number
  senderId: number
  clientMsgId: string
  /** 本期固定 'text'，插件可注册自定义类型 */
  msgType: string
  content: string
  isRecalled: boolean
  /** 服务端撤回时间（RFC3339）；撤回提示显示满 3 分钟后前端不再渲染该条消息 */
  recalledAt?: string | null
  /** 编辑时间；有值表示消息被编辑过 */
  editedAt?: string | null
  /** 单聊送达时间（客户端确认收到后由服务端回传；仅本地状态） */
  deliveredAt?: string | null
  /** 表情回应聚合（随历史分页下发） */
  reactions?: ReactionView[]
  /** 引用的消息 id（须同会话） */
  replyToId?: number | null
  /** 引用消息的摘要（服务端装配，前端直接渲染） */
  replyTo?: ReplyBrief | null
  /** 被 @ 的用户 id（仅群聊） */
  mentions?: number[]
  /** 是否是转发来的消息（气泡上标注「转发」） */
  forwarded?: boolean
  createdAt: string
  /** 前端本地状态：已发出未确认 */
  pending?: boolean
  /** 前端本地状态：发送失败 */
  failed?: boolean
}

// ===== 收藏 =====

/** 收藏夹条目（GET /api/favorites），带消息与会话的展示信息 */
export interface FavoriteItem {
  messageId: number
  conversationId: number
  convType: ConvType
  /** 会话名：群聊为群名，单聊为对方昵称 */
  convName: string
  senderId: number
  senderNickname: string
  msgType: string
  content: string
  isRecalled: boolean
  createdAt: string
  favoritedAt: string
}

// ===== 消息搜索 =====

/** 消息搜索结果（GET /api/search/messages） */
export interface SearchHit {
  messageId: number
  conversationId: number
  convType: ConvType
  convName: string
  senderId: number
  senderNickname: string
  msgType: string
  content: string
  createdAt: string
}

// ===== 好友 =====

/** 0=待申请 1=已通过 2=已拒绝 */
export type FriendStatus = 0 | 1 | 2

export interface FriendItem {
  /** 关系记录 id（user_id 维度，即 /api/friends 列表项 id） */
  id: number
  friend: Pick<User, 'id' | 'username' | 'nickname' | 'avatarColor' | 'avatar'>
  status: FriendStatus
  createdAt: string
}

export interface FriendRequestItem {
  id: number
  from: Pick<User, 'id' | 'username' | 'nickname' | 'avatarColor' | 'avatar'>
  createdAt: string
}

// ===== 插件 =====

export interface Plugin {
  id: number
  name: string
  version: string
  description: string
  permissions: string[]
  allowedHosts: string[]
  enabled: boolean
  globalDisabled: boolean
  installedBy: number
  createdAt: string
}

// ===== WebSocket 帧（§7） =====

export type ClientFrame =
  | {
      type: 'chat'
      convId: number
      clientMsgId: string
      content: string
      msgType?: string
      /** 引用的消息 id（须同会话） */
      replyToId?: number
      /** 被 @ 的用户 id（仅群聊） */
      mentions?: number[]
      forwarded?: boolean
    }
  | { type: 'typing'; convId: number }
  | { type: 'read'; convId: number; upToId: number }
  | { type: 'recall'; messageId: number }
  | { type: 'edit'; messageId: number; content: string }
  | { type: 'reaction'; messageId: number; emoji: string; action: 'set' | 'remove' }
  | { type: 'delivered'; messageId: number }
  | { type: 'ping' }

export type ServerFrame =
  | {
      type: 'message'
      id: number
      convId: number
      from: { id: number; nickname: string }
      content: string
      /** 'text' 或插件注册的自定义类型 */
      msgType: string
      clientMsgId?: string
      /** 引用摘要（服务端随帧下发，无需回查） */
      replyToId?: number
      replyTo?: ReplyBrief
      /** 被 @ 的用户 id */
      mentions?: number[]
      forwarded?: boolean
      createdAt: string
    }
  | { type: 'recall'; messageId: number; convId: number; recalledAt?: string }
  | { type: 'message_update'; id: number; convId: number; content: string; editedAt: string }
  | {
      type: 'reaction'
      messageId: number
      convId: number
      emoji: string
      /** set 时该用户此前回应的表情（可能为空） */
      before?: string
      userId: number
      action: 'set' | 'remove'
    }
  | { type: 'message_delivered'; messageId: number; convId: number; userId: number }
  | { type: 'read'; convId: number; userId: number; upToId: number }
  | { type: 'typing'; convId: number; userId: number }
  | { type: 'presence'; userId: number; online: boolean }
  /** meta=true 表示成员/群名/公告等元信息变更（需重拉成员与群文件）；纯新消息不带 */
  | { type: 'conv_change'; convId: number; meta?: boolean }
  | { type: 'kicked'; reason: string }
  | { type: 'pong' }
  | { type: 'error'; reason: string }

// ===== 描述式 UI（§8.2，白名单组件） =====

export type DeclarativeComponent =
  | 'text'
  | 'image'
  | 'button'
  | 'input'
  | 'card'
  | 'list'
  | 'divider'
  | 'link'

export interface UIDescNode {
  component: DeclarativeComponent
  props: Record<string, unknown>
  /** button/input 触发时回传给插件的动作名 */
  action?: string
  /** card/list 的子节点 */
  children?: UIDescNode[]
}

export interface PanelDesc {
  kind: 'panel'
  title: string
  body: UIDescNode[]
}

// ===== 插件 SDK 运行时（沙箱内可用的 api 对象，桥接层注入） =====

export type PluginBridgeMessage =
  | { __mc: 'call'; callId: number; api: string; args: unknown[] }
  | { __mc: 'event'; event: string; payload: unknown }
  | { __mc: 'result'; callId: number; ok: boolean; data?: unknown; error?: string }
