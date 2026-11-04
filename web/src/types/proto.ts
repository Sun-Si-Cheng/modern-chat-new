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
  /** 单聊对方的公开信息（服务端拼装）；群聊为 null */
  peer: Pick<User, 'id' | 'username' | 'nickname' | 'avatarColor' | 'avatar'> | null
  ownerId: number | null
  memberCount: number
  lastMessageId: number | null
  lastMessageAt: string | null
  lastMessagePreview: string | null
  /** 最后一条消息的类型：image/audio 时列表里显示占位文案而不是文件地址 */
  lastMessageType: string | null
  unreadCount: number
}

export interface ConversationMember {
  userId: number
  role: 'owner' | 'member'
  joinedAt: string
  user: Pick<User, 'id' | 'username' | 'nickname' | 'avatarColor' | 'avatar'>
}

// ===== 消息 =====

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
  createdAt: string
  /** 前端本地状态：已发出未确认 */
  pending?: boolean
  /** 前端本地状态：发送失败 */
  failed?: boolean
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
  | { type: 'chat'; convId: number; clientMsgId: string; content: string; msgType?: string }
  | { type: 'typing'; convId: number }
  | { type: 'read'; convId: number; upToId: number }
  | { type: 'recall'; messageId: number }
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
      createdAt: string
    }
  | { type: 'recall'; messageId: number; convId: number; recalledAt?: string }
  | { type: 'read'; convId: number; userId: number; upToId: number }
  | { type: 'typing'; convId: number; userId: number }
  | { type: 'presence'; userId: number; online: boolean }
  | { type: 'conv_change'; convId: number }
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
