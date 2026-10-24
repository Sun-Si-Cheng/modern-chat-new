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

  // ===== 消息历史（游标分页 before_id） =====

  async function openConversation(convId: number) {
    currentConvId.value = convId
    if (!messagesByConv[convId]) {
      loadingMessages.value = true
      try {
        const page = await convApi.messages(convId)
        messagesByConv[convId] = page.messages.slice().reverse()
        hasMoreByConv[convId] = page.hasMore
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
    sendFrame({ type: 'chat', convId, clientMsgId, content: trimmed })
  }

  function resend(msg: Message) {
    msg.pending = true
    msg.failed = false
    sendFrame({ type: 'chat', convId: msg.conversationId, clientMsgId: msg.clientMsgId, content: msg.content })
  }

  function sendTyping(convId: number) {
    sendFrame({ type: 'typing', convId })
  }

  function recallMessage(messageId: number) {
    sendFrame({ type: 'recall', messageId })
  }

  // ===== 上行出口（由 ws store 注入，避免循环 import） =====

  let frameSender: ((frame: ClientFrame) => void) | null = null

  function bindSender(sender: (frame: ClientFrame) => void) {
    frameSender = sender
  }

  function sendFrame(frame: ClientFrame) {
    frameSender?.(frame)
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
