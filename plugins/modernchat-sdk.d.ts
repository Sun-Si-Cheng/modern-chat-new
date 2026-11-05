/**
 * ModernChat 插件 SDK 类型声明（供 TypeScript 写插件时使用）。
 *
 * 用法：在你的 .ts 插件第一行加
 *   /// <reference path="../modernchat-sdk.d.ts" />
 * 然后照常写 `function __mcActivate(api: McApi) { ... }`。
 * 插件以单文件上传，服务端用 esbuild 去掉类型后入库，不需要本地构建。
 */

/** 会话里的一条消息（只读摘要） */
interface McMessageLite {
  id: number
  senderId: number
  msgType: string
  content: string
  createdAt: string
  recalled: boolean
}

interface McConvInfo {
  id: number
  type: 1 | 2
  name: string
  peerNickname: string
  memberCount: number
}

interface McMemberLite {
  userId: number
  nickname: string
  role: string
}

/** 右键菜单里被点击的那条消息 */
interface McActionMessage {
  id: number
  conversationId: number
  senderId: number
  msgType: string
  content: string
  createdAt: string
}

type McPrimitive = string | number | boolean | null

interface McUIDescNode {
  /** text | image | button | input | card | list | divider | link | table | progress | badge */
  component: string
  props: Record<string, McPrimitive>
  /** button / input 触发时回传给 on('ui:action') 的动作名 */
  action?: string
  children?: McUIDescNode[]
}

interface McPanelDesc {
  title?: string
  body: McUIDescNode[]
}

interface McRequestOptions {
  method?: string
  headers?: Record<string, string>
  body?: string
}

interface McResponse {
  status: number
  ok: boolean
  /** JSON 可解析时是对象，否则是原文 */
  body: unknown
}

/** 插件可监听的事件 */
type McEvent =
  | 'message:new'
  | 'message:recall'
  | 'presence'
  | 'conv:open'
  | 'ui:action'
  | 'message:action'

interface McApi {
  /** 监听事件（需 message:read 权限） */
  on(event: McEvent, fn: (payload: unknown) => void): void
  /** 发消息，msgType 可自定义（需 message:send） */
  send(convId: number, payload: unknown, msgType?: string): Promise<void>
  /** 注册自定义消息类型的渲染器（需 message:render） */
  registerMessageRenderer(type: string, fn: (payload: unknown) => McPanelDesc | McUIDescNode): Promise<void>
  /** 侧边栏面板（需 ui:panel） */
  addSidePanel(desc: McPanelDesc): Promise<void>
  /** 注册消息右键菜单项（需 ui:actions）；点击后收到 on('message:action', { actionId, message }) */
  registerMessageAction(desc: { id: string; label: string }): Promise<void>

  /** 当前用户身份：只含 id / 昵称（需 conv:read） */
  me(): Promise<{ id: number; nickname: string } | null>

  /** 当前会话只读数据（需 conv:read） */
  conv: {
    current(): Promise<McConvInfo | null>
    members(): Promise<McMemberLite[]>
  }
  messages: {
    /** 最近 n 条（1–50），按时间正序 */
    recent(n?: number): Promise<McMessageLite[]>
  }

  /** 宿主侧动作（需 ui:host） */
  nav: {
    openConversation(convId: number): Promise<void>
    jumpToMessage(messageId: number): Promise<boolean>
  }
  toast(text: string, kind?: 'info' | 'success' | 'warning' | 'error'): Promise<void>

  /** 每插件独立命名空间的键值存储（需 storage，值 ≤64KB） */
  storage: {
    get(key: string): Promise<string | null>
    set(key: string, value: string): Promise<void>
  }
  /** 经主站转发的网络请求（需 net，且域名必须在 manifest.allowedHosts 里） */
  request(url: string, opts?: McRequestOptions): Promise<McResponse>
  /** 改主题变量（需 ui:theme，仅 --td-* 令牌） */
  theme: { apply(vars: Record<string, string>): Promise<void> }
}
