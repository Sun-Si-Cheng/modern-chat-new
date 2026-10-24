import { computed, reactive, ref } from 'vue'
import { defineStore } from 'pinia'

import { convApi } from '@/api'
import type { Conversation, Message } from '@/types/proto'
import { useAuthStore } from './auth'
import type { ClientFrame, ServerFrame } from '@/types/proto'

/** 会话列表 + 消息缓存 + 收发件状态机（§9.1 链路的前端侧） */
export const useConversationsStore = defineStore('conversations', () => {
  const auth = useAuthStore()

  const conversations = ref<Conversation[]>([])
  const currentConvId = ref<number | null>(null)
  const messagesByConv = reactive<Record<number, Message[]>>({})
  const loadingMessages = ref(false)
  const hasMoreByConv = reactive<Record<number, boolean>>({})
  /** convId -> userId -> typing 过期时间戳 */
  const typingByConv = reactive<Record<number, Record<number, number>>>({})
  /** convId -> userId -> 已读到的消息 id */
  const readByConv = reactive<Record<number, Record<number, number>>>({})

  const currentConv = computed(
    () => conversations.value.find((c) => c.id === currentConvId.value) ?? null,
  )
  const currentMessages = computed(() =>
    currentConvId.value === null ? [] : (messagesByConv[currentConvId.value] ?? []),
  )

  // ===== 会话列表 =====

  async function loadConversations() {
    conversations.value = await convApi.list()
    // 服务端若返回精简字段，保证本地状态字段完整
    for (const c of conversations.value) {
      if (hasMoreByConv[c.id] === undefined) hasMoreByConv[c.id] = true
    }
    // 当前会话已不在列表中（被踢 / 已退群）：清空选择，避免展示失效会话
    if (currentConvId.value !== null && !conversations.value.some((c) => c.id === currentConvId.value)) {
      currentConvId.value = null
    }
  }

  function upsertConversation(conv: Conversation) {
    const idx = conversations.value.findIndex((c) => c.id === conv.id)
    if (idx >= 0) conversations.value[idx] = { ...conversations.value[idx], ...conv }
    else conversations.value.push(conv)
  }

  // ===== 消息历史（游标分页 beforeId） =====

  async function openConversation(convId: number) {
    currentConvId.value = convId
    if (!messagesByConv[convId]) {
      loadingMessages.value = true
      try {
        const page = await convApi.messages(convId)
        messagesByConv[convId] = page.messages.slice().reverse()
        hasMoreByConv[convId] = page.hasMore
      } catch (e) {
        // 会话已失效（被踢 / 已退群）或网络异常：回退选择，避免停在空白会话
        console.warn('[conv] open failed:', e)
        messagesByConv[convId] = []
        if (currentConvId.value === convId) currentConvId.value = null
        return
      } finally {
        loadingMessages.value = false
      }
    }
    markRead(convId)
  }

  async function loadMore(convId: number) {
    const list = messagesByConv[convId]
    if (!list || list.length === 0 || !hasMoreByConv[convId]) return
    const oldest = list[0]
    if (!oldest || oldest.id <= 0) return
    loadingMessages.value = true
    try {
      const page = await convApi.messages(convId, oldest.id)
      hasMoreByConv[convId] = page.hasMore
      if (page.messages.length) {
        messagesByConv[convId] = [...page.messages.slice().reverse(), ...list]
      }
    } catch (e) {
      // 分页失败不影响已有消息，下次滚动会重试
      console.warn('[conv] load more failed:', e)
    } finally {
      loadingMessages.value = false
    }
  }

  // ===== 已读 =====

  let lastReportedReadId = 0

  function markRead(convId: number) {
    const list = messagesByConv[convId]
    const maxId = list?.length ? Math.max(...list.map((m) => m.id).filter((i) => i > 0)) : 0
    const conv = conversations.value.find((c) => c.id === convId)
    if (conv) conv.unreadCount = 0
    if (maxId > 0 && maxId !== lastReportedReadId) {
      lastReportedReadId = maxId
      sendFrame({ type: 'read', convId, upToId: maxId })
      readByConv[convId] = { ...readByConv[convId], [auth.user?.id ?? 0]: maxId }
    }
  }

  // ===== 发送 =====

  function sendText(convId: number, content: string) {
    const trimmed = content.trim()
    if (!trimmed || trimmed.length > 1000) return
    const clientMsgId = crypto.randomUUID()
    const optimistic: Message = {
      id: -Date.now(),
      conversationId: convId,
      senderId: auth.user?.id ?? 0,
      clientMsgId,
      msgType: 'text',
      content: trimmed,
      isRecalled: false,
      createdAt: new Date().toISOString(),
      pending: true,
    }
    ;(messagesByConv[convId] ??= []).push(optimistic)
    // 连接不可用时立即标记失败，避免消息永远停在"发送中"
    const sent = sendFrame({ type: 'chat', convId, clientMsgId, content: trimmed })
    if (!sent) {
      optimistic.pending = false
      optimistic.failed = true
    }
  }

  function resend(msg: Message) {
    msg.pending = true
    msg.failed = false
    const sent = sendFrame({
      type: 'chat',
      convId: msg.conversationId,
      clientMsgId: msg.clientMsgId,
      content: msg.content,
    })
    if (!sent) {
      msg.pending = false
      msg.failed = true
    }
  }

  function sendTyping(convId: number) {
    sendFrame({ type: 'typing', convId })
  }

  function recallMessage(messageId: number) {
    sendFrame({ type: 'recall', messageId })
  }

  // ===== 断线重连后对齐服务端状态 =====

  /** 拉取最新一页并与本地合并：保留已读历史，追加断线期间的新消息 */
  async function loadLatest(convId: number) {
    const page = await convApi.messages(convId)
    // 接口按 id 倒序返回（新→旧），本地列表是正序，需先反转
    const incoming = page.messages.slice().reverse()
    const list = (messagesByConv[convId] ??= [])
    const knownIds = new Set(list.map((m) => m.id))
    const byClientId = new Map(list.filter((m) => m.clientMsgId).map((m) => [m.clientMsgId, m] as const))

    for (const m of incoming) {
      if (knownIds.has(m.id)) continue
      const local = m.clientMsgId ? byClientId.get(m.clientMsgId) : undefined
      if (local) Object.assign(local, m) // 服务端已落库：就地确认，避免重复气泡
      else list.push(m)
    }
    if (list.length) hasMoreByConv[convId] = page.hasMore
  }

  /** 重连后调用：重拉会话列表与当前会话最新消息（断线期间的增量无法从帧恢复） */
  async function resync() {
    try {
      await loadConversations()
      const id = currentConvId.value
      if (id !== null) {
        await loadLatest(id)
        markRead(id)
      }
    } catch {
      /* 忽略：用户切换会话时会重新拉取 */
    }
  }

  // ===== 上行出口（由 ws store 注入，避免循环 import） =====

  /** 返回是否真的写出（连接断开时为 false，调用方据此标记失败） */
  let frameSender: ((frame: ClientFrame) => boolean) | null = null

  function bindSender(sender: (frame: ClientFrame) => boolean) {
    frameSender = sender
  }

  function sendFrame(frame: ClientFrame): boolean {
    return frameSender?.(frame) ?? false
  }

  // ===== 下行帧处理（ws store 转发） =====

  function handleServerFrame(frame: ServerFrame) {
    switch (frame.type) {
      case 'message': {
        handleIncomingMessage(frame)
        break
      }
      case 'recall': {
        const list = messagesByConv[frame.convId]
        const msg = list?.find((m) => m.id === frame.messageId)
        if (msg) msg.isRecalled = true
        const conv = conversations.value.find((c) => c.id === frame.convId)
        if (conv && conv.lastMessageId === frame.messageId) {
          conv.lastMessagePreview = '消息已撤回'
        }
        break
      }
      case 'read': {
        ;(readByConv[frame.convId] ??= {})[frame.userId] = frame.upToId
        break
      }
      case 'typing': {
        if (frame.userId === auth.user?.id) break
        ;(typingByConv[frame.convId] ??= {})[frame.userId] = Date.now() + 3000
        break
      }
      case 'conv_change': {
        void loadConversations()
        break
      }
    }
  }

  function handleIncomingMessage(frame: Extract<ServerFrame, { type: 'message' }>) {
    const convId = frame.convId
    const list = (messagesByConv[convId] ??= [])

    // 自己发出的回显：优先按 clientMsgId 替换 pending
    if (frame.from.id === auth.user?.id) {
      const pendingIdx = frame.clientMsgId
        ? list.findIndex((m) => m.clientMsgId === frame.clientMsgId)
        : -1
      const confirmed: Message = {
        id: frame.id,
        conversationId: convId,
        senderId: frame.from.id,
        clientMsgId: frame.clientMsgId ?? '',
        msgType: frame.msgType,
        content: frame.content,
        isRecalled: false,
        createdAt: frame.createdAt,
      }
      if (pendingIdx >= 0) list.splice(pendingIdx, 1, confirmed)
      else if (!list.some((m) => m.id === frame.id)) list.push(confirmed)
      lastReportedReadId = frame.id
      return
    }

    if (list.some((m) => m.id === frame.id)) return
    list.push({
      id: frame.id,
      conversationId: convId,
      senderId: frame.from.id,
      clientMsgId: frame.clientMsgId ?? '',
      msgType: frame.msgType,
      content: frame.content,
      isRecalled: false,
      createdAt: frame.createdAt,
    })

    const conv = conversations.value.find((c) => c.id === convId)
    if (conv) {
      conv.lastMessageId = frame.id
      conv.lastMessageAt = frame.createdAt
      conv.lastMessagePreview = frame.content
      if (convId !== currentConvId.value) conv.unreadCount += 1
    }
    if (convId === currentConvId.value) markRead(convId)
  }

  // ===== 清空（登出/被踢） =====

  function reset() {
    conversations.value = []
    currentConvId.value = null
    for (const k of Object.keys(messagesByConv)) delete messagesByConv[Number(k)]
    for (const k of Object.keys(typingByConv)) delete typingByConv[Number(k)]
    for (const k of Object.keys(readByConv)) delete readByConv[Number(k)]
    lastReportedReadId = 0
  }

  return {
    conversations,
    currentConvId,
    currentConv,
    messagesByConv,
    currentMessages,
    loadingMessages,
    hasMoreByConv,
    typingByConv,
    readByConv,
    loadConversations,
    upsertConversation,
    openConversation,
    loadMore,
    resync,
    markRead,
    sendText,
    resend,
    sendTyping,
    recallMessage,
    bindSender,
    handleServerFrame,
    reset,
  }
})
