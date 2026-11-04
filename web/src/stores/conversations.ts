import { computed, reactive, ref } from 'vue'
import { defineStore } from 'pinia'

import { convApi } from '@/api'
import type { Conversation, ConversationMember, Message, ReplyBrief } from '@/types/proto'
import { useAuthStore } from './auth'
import type { ClientFrame, ServerFrame } from '@/types/proto'

/** 会话列表 + 消息缓存 + 收发件状态机（§9.1 链路的前端侧） */
export const useConversationsStore = defineStore('conversations', () => {
  const auth = useAuthStore()

  const conversations = ref<Conversation[]>([])
  const currentConvId = ref<number | null>(null)
  /**
   * 各会话的未发送草稿。
   * 切换会话时 MessagePane 会随 :key 重建，输入框组件被销毁；
   * 若不把内容提到 store，用户打了一半的话会静默丢失。
   */
  const drafts = reactive<Record<number, string>>({})
  const messagesByConv = reactive<Record<number, Message[]>>({})

  /** 记录/清除草稿（空串即清除） */
  function setDraft(convId: number, text: string) {
    if (text) drafts[convId] = text
    else delete drafts[convId]
  }

  /**
   * 待引用的消息。
   * 放在 store 而不是 MessagePane 本地：输入区（Composer）是同级组件，
   * 右键「引用」要跨组件传，走 store 比一路 emit 干净。
   */
  const replyTarget = ref<Message | null>(null)

  function setReplyTarget(m: Message | null) {
    replyTarget.value = m
  }

  // ===== 会话置顶 / 免打扰（服务端个人设置，跨设备一致）=====
  //
  // 之前置顶只存 localStorage：换设备就没了，而且和「免打扰」这类
  // 同类偏好分散在两处。现在统一走 user_conversations。

  /** 置顶会话 id（从服务端字段派生，列表排序用） */
  const pinnedIds = computed(() =>
    conversations.value.filter((c) => c.pinned).map((c) => c.id),
  )

  function isPinned(convId: number): boolean {
    return conversations.value.find((c) => c.id === convId)?.pinned === true
  }

  function isMuted(convId: number): boolean {
    return conversations.value.find((c) => c.id === convId)?.muted === true
  }

  /** 更新个人设置：只改传了的字段，返回服务端确认后的值 */
  async function setPrefs(convId: number, prefs: { pinned?: boolean; muted?: boolean }) {
    const r = await convApi.setPrefs(convId, prefs)
    const c = conversations.value.find((x) => x.id === convId)
    if (c) {
      c.pinned = r.pinned
      c.muted = r.muted
    }
  }

  async function togglePin(convId: number) {
    await setPrefs(convId, { pinned: !isPinned(convId) })
  }

  async function toggleMute(convId: number) {
    await setPrefs(convId, { muted: !isMuted(convId) })
  }

  // ===== 会话成员缓存（@ 选人 / 引用摘要里的昵称）=====

  const membersByConv = reactive<Record<number, ConversationMember[]>>({})
  const membersLoading = reactive<Record<number, boolean>>({})

  async function loadMembers(convId: number, force = false): Promise<ConversationMember[]> {
    if (!force && membersByConv[convId]) return membersByConv[convId]
    if (membersLoading[convId]) return membersByConv[convId] ?? []
    membersLoading[convId] = true
    try {
      const list = await convApi.members(convId)
      membersByConv[convId] = Array.isArray(list) ? list : []
      return membersByConv[convId]
    } catch {
      return membersByConv[convId] ?? []
    } finally {
      membersLoading[convId] = false
    }
  }

  /** 按 userId 取昵称；未加载时返回空串（调用方自行兜底） */
  function memberNickname(convId: number, userId: number): string {
    return membersByConv[convId]?.find((m) => m.user.id === userId)?.user.nickname ?? ''
  }

  /** 读回草稿 */
  function draftOf(convId: number): string {
    return drafts[convId] ?? ''
  }
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

  /** 用户资料变更（昵称/头像）后就地更新会话列表里的 peer，无需重新拉取 */
  function applyUserProfile(u: { userId: number; nickname: string; avatar: string; avatarColor: string }) {
    for (const c of conversations.value) {
      if (c.peer?.id === u.userId) {
        c.peer = { ...c.peer, nickname: u.nickname, avatar: u.avatar, avatarColor: u.avatarColor }
      }
    }
  }

  // ===== 消息历史（游标分页 beforeId） =====

  async function openConversation(convId: number) {
    currentConvId.value = convId
    if (!messagesByConv[convId]) {
      loadingMessages.value = true
      try {
        const page = await convApi.messages(convId)
        messagesByConv[convId] = (page.messages ?? []).slice().reverse()
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
    // 等消息真正落地后再上报已读：此前 upToId 只能算到 0，本地清未读而服务端仍记未读
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
      if ((page.messages ?? []).length) {
        messagesByConv[convId] = [...(page.messages ?? []).slice().reverse(), ...list]
      }
    } catch (e) {
      // 分页失败不影响已有消息，下次滚动会重试
      console.warn('[conv] load more failed:', e)
    } finally {
      loadingMessages.value = false
    }
  }

  // ===== 已读 =====

  /**
   * convId -> 已上报过的最大已读消息 id。
   * 必须按会话隔离：全局单值会导致在 A 会话上报后，B 会话的同 id 被误判为「已上报」而漏发。
   */
  const lastReportedReadByConv = new Map<number, number>()

  /** 求最大消息 id：循环而非 Math.max(...arr)，避免大数组展开导致 RangeError */
  function maxMessageId(list: Message[] | undefined): number {
    if (!list?.length) return 0
    let max = 0
    for (const m of list) if (m.id > max) max = m.id
    return max
  }

  function markRead(convId: number) {
    const list = messagesByConv[convId]
    // 消息还没加载完（或加载失败）：此时算不出 upToId，先不清本地未读，
    // 否则本地红点消失、服务端仍记未读，刷新后红点复活
    if (!list) return
    const maxId = maxMessageId(list)
    const conv = conversations.value.find((c) => c.id === convId)
    if (conv) {
      conv.unreadCount = 0
      // 服务端在 MarkRead 时一并清掉；本地同步置 false，否则「有人@我」要等刷新才消失
      conv.mentionUnread = false
    }
    if (maxId > 0 && maxId !== lastReportedReadByConv.get(convId)) {
      lastReportedReadByConv.set(convId, maxId)
      sendFrame({ type: 'read', convId, upToId: maxId })
      readByConv[convId] = { ...readByConv[convId], [auth.user?.id ?? 0]: maxId }
    }
  }

  // ===== 发送 =====

  /**
   * 发送确认超时。WS 帧可能在「连接看似正常」时丢失（服务端挂起 / 链路抖动），
   * 此时既收不到回显也收不到 error 帧，消息会永远停在 pending（转圈且没有重发入口）。
   * 超时后标记 failed，让用户能看到并重发。
   */
  const PENDING_TIMEOUT = 8000
  const pendingTimers = new Map<string, ReturnType<typeof setTimeout>>()

  function clearPendingTimer(clientMsgId: string) {
    const t = pendingTimers.get(clientMsgId)
    if (t !== undefined) {
      clearTimeout(t)
      pendingTimers.delete(clientMsgId)
    }
  }

  function armPendingTimer(msg: Message) {
    clearPendingTimer(msg.clientMsgId)
    const timer = setTimeout(() => {
      pendingTimers.delete(msg.clientMsgId)
      if (msg.pending) {
        msg.pending = false
        msg.failed = true
      }
    }, PENDING_TIMEOUT)
    pendingTimers.set(msg.clientMsgId, timer)
  }

  /** 发送附加信息：引用与 @ 都是可选 */
  interface SendExtra {
    replyToId?: number
    mentions?: number[]
  }

  /** 乐观插入 + 发帧。文本与媒体共用同一条链路，只是 msgType/content 不同。 */
  function pushAndSend(convId: number, msgType: string, content: string, extra?: SendExtra) {
    const clientMsgId = crypto.randomUUID()
    const optimistic: Message = {
      id: -Date.now(),
      conversationId: convId,
      senderId: auth.user?.id ?? 0,
      clientMsgId,
      msgType,
      content,
      isRecalled: false,
      replyToId: extra?.replyToId ?? null,
      mentions: extra?.mentions,
      createdAt: new Date().toISOString(),
      pending: true,
    }
    // 引用块要立刻显示：从当前缓存里找被引用的消息，拼一个本地摘要
    if (extra?.replyToId) {
      const target = (messagesByConv[convId] ?? []).find((m) => m.id === extra.replyToId)
      if (target) optimistic.replyTo = localReplyBrief(convId, target)
    }
    ;(messagesByConv[convId] ??= []).push(optimistic)
    // 连接不可用时立即标记失败，避免消息永远停在"发送中"
    const sent = sendFrame({
      type: 'chat',
      convId,
      clientMsgId,
      msgType,
      content,
      replyToId: extra?.replyToId,
      mentions: extra?.mentions,
    })
    if (!sent) {
      optimistic.pending = false
      optimistic.failed = true
      return
    }
    armPendingTimer(optimistic)
  }

  /** 本地构造引用摘要（仅乐观消息用；服务端回来的帧自带完整版本） */
  function localReplyBrief(convId: number, m: Message): ReplyBrief {
    const name =
      m.senderId === auth.user?.id
        ? (auth.user?.nickname ?? '')
        : memberNickname(convId, m.senderId)
    const text = m.isRecalled ? '' : [...m.content].slice(0, 60).join('')
    return {
      id: m.id,
      senderId: m.senderId,
      senderNickname: name,
      msgType: m.msgType,
      content: text,
      isRecalled: m.isRecalled,
    }
  }

  function sendText(convId: number, content: string, extra?: SendExtra) {
    const trimmed = content.trim()
    if (!trimmed || trimmed.length > 1000) return
    pushAndSend(convId, 'text', trimmed, extra)
  }

  /**
   * 发送图片 / 语音。content 是上传接口返回的相对地址，
   * 后端会再校验它必须落在本站 /uploads/ 下。
   */
  function sendMedia(convId: number, msgType: 'image' | 'audio', url: string, extra?: SendExtra) {
    if (!url) return
    pushAndSend(convId, msgType, url, extra)
  }

  function resend(msg: Message) {
    msg.pending = true
    msg.failed = false
    const sent = sendFrame({
      type: 'chat',
      convId: msg.conversationId,
      clientMsgId: msg.clientMsgId,
      // 必须带上 msgType：否则重发图片/语音会被后端按默认 text 处理，
      // 变成一条内容是 URL 的文本消息
      msgType: msg.msgType,
      content: msg.content,
    })
    if (!sent) {
      msg.pending = false
      msg.failed = true
      return
    }
    armPendingTimer(msg)
    }

  function sendTyping(convId: number) {
    sendFrame({ type: 'typing', convId })
  }

  function recallMessage(messageId: number) {
    sendFrame({ type: 'recall', messageId })
  }

  /** 「仅自己删除」成功后同步本地缓存（服务端已写删除标记） */
  function removeMessageLocal(convId: number, messageId: number) {
    const list = messagesByConv[convId]
    if (!list) return
    messagesByConv[convId] = list.filter((m) => m.id !== messageId)
  }

  // ===== 会话筛选（分组栏） =====

  const filter = ref<'all' | 'unread' | 'single' | 'group'>('all')

  function setFilter(f: 'all' | 'unread' | 'single' | 'group') {
    filter.value = f
  }

  // ===== 断线重连后对齐服务端状态 =====

  /** 拉取最新一页并与本地合并：保留已读历史，追加断线期间的新消息 */
  async function loadLatest(convId: number) {
    const page = await convApi.messages(convId)
    // 接口按 id 倒序返回（新→旧），本地列表是正序，需先反转
    const incoming = (page.messages ?? []).slice().reverse()
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
        if (msg) {
          msg.isRecalled = true
          // 服务端时间优先；老服务端/丢字段时用本地时间兜底
          msg.recalledAt = frame.recalledAt ?? new Date().toISOString()
        }
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
        replyToId: frame.replyToId ?? null,
        replyTo: frame.replyTo ?? null,
        mentions: frame.mentions,
        createdAt: frame.createdAt,
      }
      if (frame.clientMsgId) clearPendingTimer(frame.clientMsgId)
      if (pendingIdx >= 0) list.splice(pendingIdx, 1, confirmed)
      else if (!list.some((m) => m.id === frame.id)) list.push(confirmed)
      // 自己发出的消息本身就是已读位置：记住该会话的上报水位，避免重复上报
      lastReportedReadByConv.set(convId, frame.id)
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
      replyToId: frame.replyToId ?? null,
      replyTo: frame.replyTo ?? null,
      mentions: frame.mentions,
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
    filter.value = 'all'
    for (const k of Object.keys(messagesByConv)) delete messagesByConv[Number(k)]
    for (const k of Object.keys(typingByConv)) delete typingByConv[Number(k)]
    for (const k of Object.keys(readByConv)) delete readByConv[Number(k)]
    for (const k of Object.keys(drafts)) delete drafts[Number(k)]
    for (const t of pendingTimers.values()) clearTimeout(t)
    pendingTimers.clear()
    lastReportedReadByConv.clear()
  }

  return {
    conversations,
    currentConvId,
    currentConv,
    drafts,
    setDraft,
    draftOf,
    replyTarget,
    setReplyTarget,
    pinnedIds,
    isPinned,
    isMuted,
    setPrefs,
    togglePin,
    toggleMute,
    membersByConv,
    loadMembers,
    memberNickname,
    messagesByConv,
    currentMessages,
    loadingMessages,
    hasMoreByConv,
    typingByConv,
    readByConv,
    loadConversations,
    upsertConversation,
    applyUserProfile,
    openConversation,
    loadMore,
    resync,
    filter,
    setFilter,
    markRead,
    sendText,
    sendMedia,
    resend,
    sendTyping,
    recallMessage,
    removeMessageLocal,
    bindSender,
    handleServerFrame,
    reset,
  }
})
