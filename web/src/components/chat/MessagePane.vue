<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller'
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css'
import { ArrowLeftIcon, ChatIcon } from 'tdesign-icons-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import MessageComposer from './MessageComposer.vue'
import VoiceMessage from './VoiceMessage.vue'
import ContextMenu, { type ContextMenuItem } from '@/components/common/ContextMenu.vue'
import DeclarativeNodes from '@/components/declarative'
import { usePluginHost } from '@/composables/usePluginHost'
import { useAppI18n } from '@/composables/useAppI18n'
import { convApi, moderationApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useSiteStore } from '@/stores/site'
import { useConversationsStore } from '@/stores/conversations'
import { useFavoritesStore } from '@/stores/favorites'
import { useLayoutStore } from '@/stores/layout'
import { useWsStore } from '@/stores/ws'
import { FEATURES } from '@/config/features'
import type { Conversation, Message, ReplyBrief, UIDescNode } from '@/types/proto'

const auth = useAuthStore()
const convStore = useConversationsStore()
const favorites = useFavoritesStore()
const layout = useLayoutStore()
const ws = useWsStore()
const siteStore = useSiteStore()
const host = usePluginHost()
const { t, te } = useAppI18n()

const scrollerRef = ref<{ $el: HTMLElement } | null>(null)
const stickToBottom = ref(true)

const conv = computed(() => convStore.currentConv)
const messages = computed(() => convStore.currentMessages)

interface DisplayItem {
  key: string
  kind: 'date' | 'msg'
  label?: string
  msg?: Message
}

// ===== 撤回提示 =====
//
// 撤回后先显示「XXX 撤回了一条消息」，满 3 分钟把整条提示从聊天记录移除。
// 撤回时间由服务端下发并落库（recalledAt），跨刷新/跨设备一致；
// 历史遗留的撤回消息没有时间信息，按「已过期」处理（不再显示）。
const RECALL_TTL = 3 * 60 * 1000
const nowTick = ref(Date.now())
let recallTimer: ReturnType<typeof setTimeout> | undefined

function recallVisible(m: Message): boolean {
  if (!m.isRecalled) return true
  if (!m.recalledAt) return false
  const at = new Date(m.recalledAt).getTime()
  return Number.isFinite(at) && nowTick.value < at + RECALL_TTL
}

/** 撤回提示文案：自己显示「你」，别人显示其昵称 */
function recallLabel(m: Message): string {
  return senderOf(m)
    ? t('message.recalledByYou')
    : t('message.recalledBy', { name: senderName(m) })
}

// 精确调度到最近的到期时刻，不做每秒轮询（有 2000 条历史的会话也不该空转）。
// nowTick 到期后 computed 重算，提示随之下线。
watch(
  () => {
    const deadlines = messages.value
      .filter((m) => m.isRecalled && recallVisible(m))
      .map((m) => new Date(m.recalledAt as string).getTime() + RECALL_TTL)
    return deadlines.length ? Math.min(...deadlines) : 0
  },
  (deadline) => {
    clearTimeout(recallTimer)
    if (!deadline) return
    recallTimer = setTimeout(
      () => {
        nowTick.value = Date.now()
      },
      Math.max(50, deadline - Date.now() + 30),
    )
  },
  { immediate: true },
)

const displayItems = computed<DisplayItem[]>(() => {
  const out: DisplayItem[] = []
  let lastDate = ''
  for (const m of messages.value) {
    // 过期（或无时间的）撤回提示：整条从聊天记录里移除
    if (!recallVisible(m)) continue
    const d = new Date(m.createdAt).toDateString()
    if (d !== lastDate) {
      lastDate = d
      out.push({ key: `date-${d}`, kind: 'date', label: formatDateLabel(m.createdAt) })
    }
    out.push({ key: m.clientMsgId || `msg-${m.id}`, kind: 'msg', msg: m })
  }
  return out
})

function formatDateLabel(ts: string): string {
  const d = new Date(ts)
  const now = new Date()
  if (d.toDateString() === now.toDateString()) return t('message.today')
  const y = new Date(now)
  y.setDate(now.getDate() - 1)
  if (d.toDateString() === y.toDateString()) return t('message.yesterday')
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`
}

function fmtTime(ts: string): string {
  const d = new Date(ts)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// ===== 滚动 =====

function onScroll() {
  const el = scrollerRef.value?.$el as HTMLElement | undefined
  if (!el) return
  stickToBottom.value = el.scrollTop + el.clientHeight >= el.scrollHeight - 80
  if (el.scrollTop < 40 && conv.value && convStore.hasMoreByConv[conv.value.id] && !convStore.loadingMessages) {
    void convStore.loadMore(conv.value.id)
  }
}

function scrollToBottom() {
  const pin = () => {
    const el = scrollerRef.value?.$el as HTMLElement | undefined
    if (el) el.scrollTop = el.scrollHeight
  }
  void nextTick(() => {
    pin()
    // DynamicScroller 先按 min-item-size 估算、之后才实测真实高度，
    // 首帧的 scrollHeight 会偏小 —— 只滚一次常常停在历史中段，补两帧校正
    requestAnimationFrame(() => {
      pin()
      requestAnimationFrame(pin)
    })
  })
}

watch(
  () => messages.value.length,
  () => {
    if (stickToBottom.value) scrollToBottom()
  },
)

// 贴在底部 = 用户在读最新消息；上翻历史时暂停自动已读，回到末尾再补偿
watch(
  stickToBottom,
  (atBottom) => {
    convStore.setAutoReadPaused(!atBottom)
    if (atBottom && conv.value) convStore.markRead(conv.value.id)
  },
  { immediate: true },
)

watch(
  () => conv.value?.id,
  (id) => {
    stickToBottom.value = true
    scrollToBottom()
    if (id !== undefined) host.notifyConvOpen(id)
  },
  { immediate: true },
)

/** 从后台标签页切回：此时才把当前会话的新消息标为已读 */
function onVisibilityChange() {
  if (document.visibilityState !== 'visible') return
  if (stickToBottom.value && conv.value) convStore.markRead(conv.value.id)
}

/** Ctrl/Cmd+F：打开详情面板并聚焦「搜索聊天记录」（搜索入口在 DetailPanel） */
function onPaneKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'f' && conv.value) {
    e.preventDefault()
    // 抽屉形态且收起时先展开（不要用 toggle，避免已展开时被关掉）
    if (layout.panes.detail.drawer && !layout.opened.detail) layout.open('detail')
    layout.requestDetailSearch()
    return
  }
  // Esc 关闭转发选择框 / 举报浮层：它们盖在最上层且原来不响应键盘，键盘用户会被困住
  if (e.key === 'Escape' && (forwardSource.value || mergeSourceIds.value)) {
    e.preventDefault()
    closeForwardPicker()
    return
  }
  if (e.key === 'Escape' && reportTarget.value) {
    e.preventDefault()
    reportTarget.value = null
  }
}

onMounted(() => {
  scrollToBottom()
  window.addEventListener('keydown', onPaneKeydown)
  document.addEventListener('visibilitychange', onVisibilityChange)
})

// ===== 群成员昵称缓存 =====
//
// 组件按会话重建（ChatLayout 上带 :key），所以缓存天然按会话隔离；
// 额外加一个失败标记：接口 5xx 时不要在每次渲染里反复重打 members 接口。

const memberNameCache = reactive<Record<number, string>>({})
let memberNamesFailed = false
/** 我在当前群的角色（判断是否显示「删除违规消息」）；单聊为空 */
const myGroupRole = ref('')

/** 站点管理员或当前群群主/管理员可删除违规消息 */
const canModerate = computed(() => {
  if (auth.user?.role === 'admin') return true
  return conv.value?.type === 2 && (myGroupRole.value === 'owner' || myGroupRole.value === 'admin')
})

let memberNamesLoading = false

watch(
  () => conv.value?.id,
  (id) => {
    myGroupRole.value = ''
    if (id === undefined || conv.value?.type !== 2) return
    void loadMemberNames()
  },
  { immediate: true },
)

function memberName(userId: number): string {
  if (memberNameCache[userId]) return memberNameCache[userId]
  // 失败过就不再随渲染重试，避免接口挂掉时每帧打一次请求
  if (!memberNamesFailed) void loadMemberNames()
  return t('message.userFallback', { n: userId })
}

async function loadMemberNames() {
  if (memberNamesLoading || !conv.value || memberNamesFailed) return
  memberNamesLoading = true
  try {
    // 复用 store 的成员缓存（自带 in-flight 去重）：
    // 同一份 /conversations/:id/members 原来会被输入区 @ 选人、详情面板和这里各拉一次
    const members = await convStore.loadMembers(conv.value.id)
    if (!members.length) {
      memberNamesFailed = true
      return
    }
    for (const m of members) {
      memberNameCache[m.userId] = m.user.nickname
      if (m.userId === auth.user?.id) myGroupRole.value = m.role
    }
  } catch {
    memberNamesFailed = true
  } finally {
    memberNamesLoading = false
  }
}

// 群主转让 / 被降级只发 conv_change(meta)，不会换会话：
// 这里主动刷新一次角色与昵称，否则右键菜单里的「删除违规消息」会一直按旧角色显示
watch(
  () => convStore.convChangeTick,
  () => {
    const c = conv.value
    if (!c || c.type !== 2) return
    memberNamesFailed = false
    for (const k of Object.keys(memberNameCache)) delete memberNameCache[Number(k)]
    void convStore.loadMembers(c.id, true).then((members) => {
      for (const m of members) {
        memberNameCache[m.userId] = m.user.nickname
        if (m.userId === auth.user?.id) myGroupRole.value = m.role
      }
    })
  },
)

// ===== 撤回 =====

function canRecall(m: Message): boolean {
  if (m.isRecalled || m.senderId !== auth.user?.id || m.id <= 0) return false
  // 撤回时限跟随后台配置（0 = 关闭撤回）；本地判断只做提示，
  // 服务端仍会再校验一次，两端不一致时以服务端为准
  const minutes = siteStore.config.recallMinutes
  if (minutes <= 0) return false
  return Date.now() - new Date(m.createdAt).getTime() < minutes * 60 * 1000
}

function recall(m: Message) {
  convStore.recallMessage(m.id)
  void MessagePlugin.success(t('message.recallOk'))
}

// ===== 编辑 =====

const EDIT_WINDOW = 5 * 60 * 1000

function canEdit(m: Message): boolean {
  if (m.isRecalled || m.senderId !== auth.user?.id || m.msgType !== 'text' || m.id <= 0) return false
  if (m.pending || m.failed) return false
  return Date.now() - new Date(m.createdAt).getTime() < EDIT_WINDOW
}

function startEdit(m: Message) {
  convStore.setReplyTarget(null)
  convStore.setEditingMessage(m)
}

// ===== 表情回应 =====

const REACTION_EMOJIS = ['👍', '❤️', '😂', '😮', '😢', '🎉']
const reactionPicker = ref(0)

function toggleReactionPicker(messageId: number) {
  reactionPicker.value = reactionPicker.value === messageId ? 0 : messageId
}

function hasMine(m: Message, emoji: string): boolean {
  return (m.reactions ?? []).some((r) => r.emoji === emoji && r.mine)
}

function react(m: Message, emoji: string) {
  reactionPicker.value = 0
  convStore.toggleReaction(m.id, emoji, hasMine(m, emoji))
}

// 切换会话时收起表情选择器
watch(
  () => conv.value?.id,
  () => {
    reactionPicker.value = 0
  },
)

// ===== 群公告 =====
const announceOpen = ref(false)

/** 「谁 · 什么时候」：时间缺失时只显示发布者，避免 Invalid Date */
const announceMeta = computed(() => {
  const c = conv.value
  if (!c) return ''
  const who = c.announcementBy || ''
  if (!c.announcementAt) return who
  const d = new Date(c.announcementAt)
  const day = `${d.getMonth() + 1}月${d.getDate()}日`
  return `${who} · ${day} ${fmtTime(c.announcementAt)}`
})

// ===== 多选 / 合并转发 =====

const selectMode = ref(false)
const selectedIds = ref<number[]>([])
/** 非 null 时表示正在为「合并转发」选择目标会话 */
const mergeSourceIds = ref<number[] | null>(null)

const MAX_MERGE = 20

function enterSelect(m: Message) {
  selectMode.value = true
  selectedIds.value = m.id > 0 ? [m.id] : []
}

function exitSelect() {
  selectMode.value = false
  selectedIds.value = []
}

function toggleSelect(m: Message) {
  if (m.id <= 0) return
  const i = selectedIds.value.indexOf(m.id)
  if (i >= 0) {
    selectedIds.value.splice(i, 1)
    return
  }
  // 合并转发有 20 条上限：超了必须当场拒绝，否则会静默丢掉后面的消息
  if (selectedIds.value.length >= MAX_MERGE) {
    void MessagePlugin.warning(t('message.mergeLimit', { max: MAX_MERGE }))
    return
  }
  selectedIds.value.push(m.id)
}

function isSelected(m: Message): boolean {
  return selectedIds.value.includes(m.id)
}

/** 媒体消息在多选模式下点击=选中，否则维持原来的预览行为 */
function onMediaClick(m: Message, e?: MouseEvent) {
  if (selectMode.value) {
    // 外层消息行也有「多选时点击=选中」的处理：不阻止冒泡会选中再取消，
    // 表现为图片/表情在多选模式下永远选不上
    e?.stopPropagation()
    toggleSelect(m)
    return
  }
  previewSrc.value = m.content
}

/** 把选中的消息拼成一条「合并转发」文本；媒体用占位文案代替 URL */
function mergeText(msgs: Message[]): string {
  const lines = msgs.slice(0, MAX_MERGE).map((m) => {
    let body = m.content
    if (m.msgType === 'image') body = t('message.quoteImage')
    else if (m.msgType === 'audio') body = t('message.quoteAudio')
    else if (m.msgType === 'sticker') body = t('message.quoteSticker')
    else if (m.msgType === 'video') body = t('message.quoteVideo')
    else if (m.msgType !== 'text') body = t('message.unknownType', { type: m.msgType })
    body = [...body].slice(0, 100).join('')
    const who = senderOf(m) ? (auth.user?.nickname ?? '') : senderName(m)
    return `${who}: ${body}`
  })
  return `【${t('message.mergeForward')}】\n${lines.join('\n')}`
}

function startMergeForward() {
  if (!selectedIds.value.length) return
  mergeSourceIds.value = [...selectedIds.value]
}

function doMergeForward(convId: number) {
  const ids = mergeSourceIds.value
  if (!ids) return
  const picked = messages.value.filter((m) => ids.includes(m.id))
  // 选中之后原消息可能被撤回：撤回的内容不能再转发出去
  const msgs = picked.filter((m) => !m.isRecalled)
  if (!msgs.length) {
    mergeSourceIds.value = null
    void MessagePlugin.warning(t('message.forwardRecalled'))
    return
  }
  const skipped = picked.length - msgs.length
  const text = mergeText(msgs)
  if (text.length > siteStore.config.maxMessageLength) {
    void MessagePlugin.warning(t('message.mergeTooLong', { max: siteStore.config.maxMessageLength }))
    return
  }
  convStore.sendText(convId, text, { forwarded: true })
  mergeSourceIds.value = null
  exitSelect()
  if (skipped > 0) void MessagePlugin.info(t('message.mergeSkippedRecalled', { n: skipped }))
  void MessagePlugin.success(t('message.forwardOk'))
}

/** 目标会话选择框的点击：普通转发与合并转发共用 */
function pickForwardTarget(convId: number) {
  if (mergeSourceIds.value) doMergeForward(convId)
  else doForward(convId)
}

function closeForwardPicker() {
  forwardSource.value = null
  mergeSourceIds.value = null
}

// ===== 转发 =====

/** 待选择目标会话的消息；非 null 时弹出会话选择框 */
const forwardSource = ref<Message | null>(null)
const forwardPanelRef = ref<HTMLElement | null>(null)

// 浮层打开时把焦点移入：否则键盘用户要 Tab 穿过多层节点才能真正选目标
watch(
  () => forwardSource.value || mergeSourceIds.value,
  (open) => {
    if (open) void nextTick(() => forwardPanelRef.value?.focus())
  },
)

const forwardTargets = computed(() =>
  // type=3 是系统公告会话，不能往里转发
  convStore.conversations.filter((c) => c.type !== 3),
)

/** 禁止选择的目标会话：普通转发不能发回原会话；合并转发同理 */
const forwardDisabledId = computed(() =>
  mergeSourceIds.value ? conv.value?.id : forwardSource.value?.conversationId,
)

function convLabel(c: Conversation): string {
  return c.name || c.peer?.nickname || t('message.forwardUnknownConv')
}

function doForward(convId: number) {
  const m = forwardSource.value
  if (!m) return
  // 选择目标会话期间原消息可能被撤回：发之前再判一次，别把撤回内容发出去
  if (m.isRecalled) {
    forwardSource.value = null
    void MessagePlugin.warning(t('message.forwardRecalled'))
    return
  }
  const ok = convStore.forwardMessage(convId, m)
  forwardSource.value = null
  if (ok) void MessagePlugin.success(t('message.forwardOk'))
  else void MessagePlugin.error(t('message.forwardSameConv'))
}

// ===== @ 高亮 =====

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * 把正文按「@昵称」切段，高亮其中的 @ 部分。
 * 只在消息确实带 mentions 时才做，普通文本走原样渲染，不额外开销。
 */
function textParts(m: Message): { t: string; at: boolean }[] {
  if (m.msgType !== 'text' || !m.mentions?.length) return [{ t: m.content, at: false }]
  const names = m.mentions.map((id) => memberName(id)).filter(Boolean)
  if (!names.length) return [{ t: m.content, at: false }]
  const re = new RegExp(`(@(?:${names.map(escapeRe).join('|')}))`, 'g')
  return m.content
    .split(re)
    .filter((s) => s !== '')
    .map((s) => ({ t: s, at: names.some((n) => s === `@${n}`) }))
}

// ===== 引用 =====

/** 引用块里显示的文本：媒体与已撤回要给占位，不能直接甩 URL */
function quoteText(r: ReplyBrief): string {
  if (r.isRecalled) return t('message.quoteRecalled')
  if (r.msgType === 'image') return t('message.quoteImage')
  if (r.msgType === 'audio') return t('message.quoteAudio')
  if (r.msgType === 'sticker') return t('message.quoteSticker')
  if (r.msgType === 'video') return t('message.quoteVideo')
  return r.content
}

/** 被点中的引用目标，短暂高亮一下，否则跳过去也找不到是哪条 */
const flashId = ref(0)

function jumpToMessage(id: number) {
  const idx = displayItems.value.findIndex((it) => it.kind === 'msg' && it.msg?.id === id)
  if (idx < 0) return
  const sc = scrollerRef.value as unknown as { scrollToItem?: (i: number) => void } | null
  sc?.scrollToItem?.(idx)
  flashId.value = id
  window.setTimeout(() => {
    if (flashId.value === id) flashId.value = 0
  }, 1200)
}

// 搜索结果跨会话跳转：消息页加载到位后滚动并高亮
let jumpTimer: number | undefined
watch(
  [() => convStore.pendingJumpId, () => displayItems.value.length],
  () => {
    const id = convStore.pendingJumpId
    if (!id) {
      window.clearTimeout(jumpTimer)
      return
    }
    if (displayItems.value.some((it) => it.kind === 'msg' && it.msg?.id === id)) {
      window.clearTimeout(jumpTimer)
      void nextTick(() => {
        jumpToMessage(id)
        convStore.pendingJumpId = null
      })
      return
    }
    // 兜底：目标消息可能已被移除（撤回过期/被删），等不到就清掉标记，
    // 否则残留的 pendingJumpId 会让下一次跳转带着脏状态
    window.clearTimeout(jumpTimer)
    jumpTimer = window.setTimeout(() => {
      if (convStore.pendingJumpId === id) convStore.pendingJumpId = null
    }, 1500)
  },
)

// ===== 图片预览（灯箱）=====

const previewSrc = ref('')

function onPreviewKey(e: KeyboardEvent) {
  if (e.key === 'Escape') previewSrc.value = ''
}

watch(previewSrc, (v) => {
  if (v) window.addEventListener('keydown', onPreviewKey)
  else window.removeEventListener('keydown', onPreviewKey)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onPreviewKey)
  window.removeEventListener('keydown', onPaneKeydown)
  document.removeEventListener('visibilitychange', onVisibilityChange)
  // 离开聊天页时恢复自动已读，避免这个开关一直挂在 store 上影响别的入口
  convStore.setAutoReadPaused(false)
  clearTimeout(recallTimer)
  window.clearTimeout(jumpTimer)
})

// ===== 消息右键菜单 =====

const menuVisible = ref(false)
const menuX = ref(0)
const menuY = ref(0)
const menuTarget = ref<Message | null>(null)

/** 已撤回、尚未落库（pending）的消息不弹菜单：没有可执行的操作 */
function openMenu(e: MouseEvent, m: Message) {
  if (m.isRecalled || m.id <= 0) return
  imageTarget.value = null
  menuTarget.value = m
  menuX.value = e.clientX
  menuY.value = e.clientY
  menuVisible.value = true
}

/** 图片右键菜单：与消息菜单共用一套定位状态，只是条目不同 */
const imageTarget = ref<Message | null>(null)

function openImageMenu(e: MouseEvent, m: Message) {
  imageTarget.value = m
  // 图片菜单也复用消息的通用操作（引用 / 转发 / 收藏 / 撤回 / 删除），
  // 所以 menuTarget 必须一起设置，否则这些项点了没有目标
  menuTarget.value = m
  menuX.value = e.clientX
  menuY.value = e.clientY
  menuVisible.value = true
}

async function saveImage(m: Message) {
  try {
    // 走 fetch 取 blob 再下载：<a download> 对跨域（对象存储）不生效
    const res = await fetch(m.content)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const blob = await res.blob()
    const ext = /\.(\w{2,5})(?:\?|$)/.exec(m.content)?.[1] ?? 'png'
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `image-${m.id}.${ext}`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
    void MessagePlugin.success(t('message.saveImageOk'))
  } catch (e) {
    void MessagePlugin.error(`${t('message.saveImageFailed')}：${String(e)}`)
  }
}

/**
 * 消息的通用操作项（文本 / 图片 / 表情共用）。
 *
 * 抽出来是因为图片菜单原来只有「查看大图 / 保存图片 / 收藏」，
 * 而引用、转发、撤回、删除在图片上同样需要——两处各写一遍必然分叉。
 * dividerFirst：图片菜单前面还有专属项，第一项要加分隔线。
 */
function commonActions(m: Message, dividerFirst: boolean): ContextMenuItem[] {
  const items: ContextMenuItem[] = []
  const markDivider = () => {
    if (dividerFirst && items.length === 0) return true
    return false
  }

  // 撤回提示、未落库的乐观消息不参与引用 / 转发 / 删除
  const actionable = !m.isRecalled && m.id > 0
  if (actionable) {
    items.push({ key: 'quote', label: t('message.quote'), dividerBefore: markDivider() })
    items.push({ key: 'forward', label: t('message.forward') })
    if (canEdit(m)) items.push({ key: 'edit', label: t('message.edit') })
  }
  items.push({
    key: 'favorite',
    label: favorites.isFavorited(m.id) ? t('message.unfavorite') : t('message.favorite'),
    dividerBefore: markDivider(),
  })
  if (canRecall(m)) {
    items.push({
      key: 'recall',
      label: t('message.recall'),
      danger: true,
      dividerBefore: items.length > 0,
    })
  }
  if (actionable) {
    items.push({ key: 'select', label: t('message.select'), dividerBefore: true })
    items.push({ key: 'delete', label: t('message.delete'), danger: true })
    // 举报他人消息（自己的消息不提供举报入口）
    if (!senderOf(m)) {
      items.push({ key: 'report', label: t('message.report'), danger: true })
    }
    if (canModerate.value) {
      items.push({ key: 'adminDelete', label: t('message.adminDelete'), danger: true })
    }
  }
  return items
}

/** 插件注册的右键菜单项：键前缀区分，回传给对应插件的运行时 */
const PLUGIN_ACTION_PREFIX = 'plug:'

function pluginActionItems(m: Message, dividerFirst: boolean): ContextMenuItem[] {
  if (m.isRecalled || m.id <= 0) return []
  return host.pluginActions().map((a, i) => ({
    key: `${PLUGIN_ACTION_PREFIX}${a.pluginId}:${a.id}`,
    label: a.label,
    dividerBefore: i === 0 && dividerFirst,
  }))
}

const menuItems = computed<ContextMenuItem[]>(() => {
  const m = menuTarget.value
  if (!m) return []

  // 图片 / 表情：专属项（查看大图、保存）在前，通用操作在后
  if (imageTarget.value) {
    const imageOnly: ContextMenuItem[] = [
      { key: 'imgOpen', label: t('message.openImage') },
      { key: 'imgSave', label: t('message.saveImage') },
    ]
    // 还没落库的消息（上传中）没有可操作的通用项
    if (m.id <= 0) return imageOnly
    return [...imageOnly, ...commonActions(m, true), ...pluginActionItems(m, true)]
  }

  const items: ContextMenuItem[] = []
  if (m.msgType === 'text' && m.content) {
    items.push({ key: 'copy', label: t('message.copy') })
  }
  items.push(...commonActions(m, false))
  items.push(...pluginActionItems(m, true))
  return items
})

async function copyMessage(m: Message) {
  const text = m.content ?? ''
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    void MessagePlugin.success(t('message.copied'))
  } catch {
    // 非安全上下文（http）下 clipboard 不可用，退回 execCommand
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    if (ok) void MessagePlugin.success(t('message.copied'))
    else void MessagePlugin.error(t('message.copyFailed'))
  }
}

function onMenuSelect(key: string) {
  const m = menuTarget.value
  if (!m) return

  // 图片 / 表情专属项（其余键落到下面的通用处理）
  if (imageTarget.value) {
    if (key === 'imgOpen') {
      previewSrc.value = m.content
      return
    }
    if (key === 'imgSave') {
      void saveImage(m)
      return
    }
  }

  // 插件动作：plug:<pluginId>:<actionId>，只投递给该插件
  if (key.startsWith(PLUGIN_ACTION_PREFIX)) {
    const [, pluginId, ...rest] = key.slice(PLUGIN_ACTION_PREFIX.length).split(':')
    host.runMessageAction(Number(pluginId), rest.join(':'), {
      id: m.id,
      conversationId: m.conversationId,
      senderId: m.senderId,
      msgType: m.msgType,
      content: m.content,
      createdAt: m.createdAt,
    })
    return
  }

  if (key === 'copy') void copyMessage(m)
  if (key === 'quote') convStore.setReplyTarget(m)
  if (key === 'forward') forwardSource.value = m
  if (key === 'edit') startEdit(m)
  if (key === 'recall') recall(m)
  if (key === 'favorite') void toggleFavorite(m)
  if (key === 'delete') confirmDelete(m)
  if (key === 'report') startReport(m)
  if (key === 'adminDelete') confirmAdminDelete(m)
  if (key === 'select') enterSelect(m)
}

// ===== 收藏 / 删除（仅自己） =====

async function toggleFavorite(m: Message) {
  try {
    if (favorites.isFavorited(m.id)) {
      await favorites.remove(m.id)
      void MessagePlugin.success(t('message.unfavoriteOk'))
    } else {
      await favorites.add(m.id)
      void MessagePlugin.success(t('message.favoriteOk'))
    }
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
}

function confirmDelete(m: Message) {
  const dialog = DialogPlugin.confirm({
    header: t('message.deleteTitle'),
    body: t('message.deleteBody'),
    confirmBtn: { content: t('message.delete'), theme: 'danger' },
    onConfirm: async () => {
      try {
        await convApi.deleteMessage(m.conversationId, m.id)
        convStore.removeMessageLocal(m.conversationId, m.id)
        // 会话预览与未读可能随之变化，重新拉取列表
        await convStore.loadConversations().catch(() => undefined)
        void MessagePlugin.success(t('message.deleteOk'))
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}

// ===== 举报 / 管理员删除违规消息 =====

const reportTarget = ref<Message | null>(null)
const reportReason = ref('')
const reporting = ref(false)

function startReport(m: Message) {
  reportTarget.value = m
  reportReason.value = ''
}

async function submitReport() {
  const m = reportTarget.value
  if (!m) return
  const reason = reportReason.value.trim()
  if (!reason) {
    void MessagePlugin.warning(t('message.reportReasonEmpty'))
    return
  }
  reporting.value = true
  try {
    const r = await moderationApi.report(m.id, reason)
    void MessagePlugin.success(r.duplicate ? t('message.reportDuplicate') : t('message.reportOk'))
    reportTarget.value = null
  } catch (e) {
    void MessagePlugin.error(te(e))
  } finally {
    reporting.value = false
  }
}

function confirmAdminDelete(m: Message) {
  const dialog = DialogPlugin.confirm({
    header: t('message.adminDeleteTitle'),
    body: t('message.adminDeleteBody'),
    confirmBtn: { content: t('message.adminDelete'), theme: 'danger' },
    onConfirm: async () => {
      try {
        await moderationApi.adminDelete(m.id)
        convStore.removeMessageLocal(m.conversationId, m.id)
        await convStore.loadConversations().catch(() => undefined)
        void MessagePlugin.success(t('message.adminDeleteOk'))
      } catch (e) {
        void MessagePlugin.error(te(e))
      }
      dialog.destroy()
    },
  })
}

// ===== 已读回执（单聊） =====

const peerReadUpTo = computed(() => {
  const c = conv.value
  if (!c || c.type !== 1 || !c.peer) return 0
  return convStore.readByConv[c.id]?.[c.peer.id] ?? 0
})

/** 单聊消息状态：发送中 → 已发送 → 已送达 → 已读 */
function messageStatus(m: Message): string {
  if (m.failed) return t('message.sendFailed')
  if (m.pending) return t('message.sending')
  if (peerReadUpTo.value >= m.id) return t('message.read')
  if (m.deliveredAt) return t('message.delivered')
  return t('message.sent')
}

/** 失败消息重发：复用 store 的 resend（重置 pending 并重新发帧） */
function retrySend(m: Message) {
  if (!m.failed) return
  convStore.resend(m)
}

// ===== 正在输入 =====

const typingNames = computed(() => {
  const c = conv.value
  if (!c) return []
  const map = convStore.typingByConv[c.id] ?? {}
  const now = Date.now()
  return Object.entries(map)
    .filter(([, expiry]) => expiry > now)
    .map(([userId]) => (c.type === 1 ? (c.peer?.nickname ?? '') : memberName(Number(userId))))
})

// ===== 自定义消息类型：插件渲染（§8.2） =====

interface RenderedDesc {
  nodes: UIDescNode[]
}

const renderedMap = reactive<Record<number, RenderedDesc | 'failed' | undefined>>({})

function ensureRendered(m: Message) {
  if (m.msgType === 'text' || m.isRecalled) return
  if (renderedMap[m.id] !== undefined) return
  if (!host.hasRenderer(m.msgType)) {
    renderedMap[m.id] = 'failed'
    return
  }
  let payload: unknown = m.content
  try {
    payload = JSON.parse(m.content)
  } catch {
    /* 非 JSON 原样传给插件 */
  }
  host
    .requestRender(m.msgType, payload)
    .then((panel) => {
      renderedMap[m.id] = panel ? { nodes: panel.body } : 'failed'
    })
    .catch(() => {
      renderedMap[m.id] = 'failed'
    })
}

watch(
  displayItems,
  (items) => {
    for (const it of items) if (it.kind === 'msg' && it.msg) ensureRendered(it.msg)
  },
  { immediate: true },
)

function isRenderedOk(m: Message): boolean {
  const r = renderedMap[m.id]
  return !!r && r !== 'failed'
}

function renderedNodes(m: Message): RenderedDesc['nodes'] {
  const r = renderedMap[m.id]
  return r && r !== 'failed' ? r.nodes : []
}

const senderOf = (m: Message) => m.senderId === auth.user?.id
const senderAvatarColor = (m: Message) => {
  if (senderOf(m)) return auth.user?.avatarColor || '#0052d9'
  const c = conv.value
  if (c?.type === 1 && c.peer?.id === m.senderId) return c.peer.avatarColor
  return '#7b8290'
}

/** 对方自定义头像（单聊取 peer；群聊成员头像由详情面板展示） */
const senderAvatar = (m: Message) => {
  if (senderOf(m)) return auth.user?.avatar ?? ''
  const c = conv.value
  if (c?.type === 1 && c.peer?.id === m.senderId) return c.peer.avatar ?? ''
  return ''
}

/** 头像下的名字：自己用昵称，单聊用对方昵称，群聊查成员表 */
const senderName = (m: Message) => {
  if (senderOf(m)) return auth.user?.nickname || auth.user?.username || '?'
  if (conv.value?.type === 1) return conv.value.peer?.nickname ?? '?'
  return memberName(m.senderId)
}
</script>

<template>
  <section class="mc-msgpane">
    <header class="mc-msgpane__header">
      <UiButton
        class="mc-msgpane__back"
        variant="text"
        shape="square"
        :aria-label="t('message.backToConversations')"
        @click="convStore.currentConvId = null"
      >
        <template #icon><ArrowLeftIcon /></template>
      </UiButton>
      <div v-if="conv" class="mc-msgpane__identity">
        <AvatarBadge
          :name="conv.type === 1 ? (conv.peer?.nickname ?? '?') : (conv.name ?? '#')"
          :src="conv.type === 1 ? conv.peer?.avatar : conv.avatar"
          :color="conv.type === 1 ? (conv.peer?.avatarColor ?? '#0052d9') : '#2ba471'"
          :online="conv.type === 1 && conv.peer ? ws.onlineUsers.has(conv.peer.id) : false"
          :size="32"
        />
        <div class="mc-msgpane__title">
          <span class="mc-msgpane__name">
            {{ conv.type === 1 ? conv.peer?.nickname : conv.name }}
          </span>
          <span class="text-placeholder mc-msgpane__sub">
            {{
              conv.type === 1
                ? conv.peer && ws.onlineUsers.has(conv.peer.id)
                  ? t('chat.online')
                  : t('chat.offline')
                : t('chat.membersCount', { n: conv.memberCount })
            }}
          </span>
        </div>
      </div>
    </header>

    <!-- 群公告：折叠时一行摘要，展开看全文与发布者 -->
    <div
      v-if="conv?.type === 2 && conv.announcement"
      class="mc-announce"
      :class="{ 'is-open': announceOpen }"
    >
      <button
        type="button"
        class="mc-announce__head"
        :aria-expanded="announceOpen"
        @click="announceOpen = !announceOpen"
      >
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <path
            d="M4 10v4a2 2 0 0 0 2 2h1l2.5 4 1.7-1-2-3H16l3 3V5l-3 3H7a3 3 0 0 0-3 2Z"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linejoin="round"
          />
        </svg>
        <span class="mc-announce__label">{{ t('chat.announcement') }}</span>
        <span v-if="!announceOpen" class="mc-announce__preview">{{ conv.announcement }}</span>
        <span class="mc-announce__toggle">
          {{ announceOpen ? t('chat.collapse') : t('chat.expand') }}
        </span>
      </button>
      <div v-if="announceOpen" class="mc-announce__body">
        <p class="mc-announce__text">{{ conv.announcement }}</p>
        <p class="mc-announce__meta text-placeholder">{{ announceMeta }}</p>
      </div>
    </div>

    <!-- 多选工具条：选中后可合并转发 -->
    <div v-if="selectMode" class="mc-selectbar">
      <span class="mc-selectbar__count">
        {{ t('message.selectedCount', { n: selectedIds.length }) }}
      </span>
      <UiButton size="small" variant="text" @click="exitSelect">{{ t('common.cancel') }}</UiButton>
      <UiButton
        size="small"
        theme="primary"
        :disabled="!selectedIds.length"
        @click="startMergeForward"
      >
        {{ t('message.mergeForward') }}
      </UiButton>
    </div>

    <DynamicScroller
      v-if="conv && (messages.length || convStore.loadingMessages)"
      ref="scrollerRef"
      class="mc-msgpane__list"
      :items="displayItems"
      :min-item-size="88"
      key-field="key"
      @scroll="onScroll"
    >
      <template #before>
        <div v-if="convStore.loadingMessages" class="text-placeholder mc-msgpane__loading">
          {{ t('common.loading') }}
        </div>
        <div v-else-if="!convStore.hasMoreByConv[conv.id]" class="text-placeholder mc-msgpane__loading">
          {{ t('message.noEarlier') }}
        </div>
      </template>

      <template #default="{ item }">
        <DynamicScrollerItem :item="item" :active="true">
          <template v-if="item.kind === 'date'">
            <div class="mc-msgpane__date text-placeholder">{{ item.label }}</div>
          </template>
          <template v-else-if="item.msg">
            <div
              class="mc-msg"
              :class="{
                mine: senderOf(item.msg),
                'is-recalled': item.msg.isRecalled,
                'is-flash': flashId === item.msg.id,
                'is-selected': selectMode && isSelected(item.msg),
                'is-selecting': selectMode,
              }"
              @click="selectMode && toggleSelect(item.msg)"
              @contextmenu.prevent="openMenu($event, item.msg)"
            >
              <!-- 多选标记 -->
              <span
                v-if="selectMode && !item.msg.isRecalled"
                class="mc-msg__check"
                :class="{ 'is-on': isSelected(item.msg) }"
                aria-hidden="true"
              >✓</span>
              <!-- 头像：自己的消息也显示（.mine 的 row-reverse 会把它排到最右）。
                   撤回提示是系统文案，居中显示，不带头像。 -->
              <AvatarBadge
                v-if="!item.msg.isRecalled"
                :name="senderName(item.msg)"
                :src="senderAvatar(item.msg)"
                :color="senderAvatarColor(item.msg)"
                :size="32"
              />
              <div class="mc-msg__body">
                <div
                  v-if="conv?.type === 2 && !senderOf(item.msg) && !item.msg.isRecalled"
                  class="mc-msg__sender text-placeholder"
                >
                  {{ memberName(item.msg.senderId) }}
                </div>

                <!-- 转发标注：转发不保留原作者，只标来源性质 -->
                <div v-if="item.msg.forwarded && !item.msg.isRecalled" class="mc-msg__forward">
                  <svg width="11" height="11" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                    <path
                      d="M4 8h11a5 5 0 0 1 0 10H8M4 8l4-4M4 8l4 4"
                      stroke="currentColor"
                      stroke-width="1.6"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                    />
                  </svg>
                  {{ t('message.forwarded') }}
                </div>

                <!-- 引用：点一下跳到原消息 -->
                <div
                  v-if="item.msg.replyTo && !item.msg.isRecalled"
                  class="mc-msg__quote"
                  role="button"
                  tabindex="0"
                  @click="jumpToMessage(item.msg.replyTo.id)"
                  @keydown.enter="jumpToMessage(item.msg.replyTo.id)"
                >
                  <span class="mc-msg__quote-name">{{ item.msg.replyTo.senderNickname || '…' }}</span>
                  <span class="mc-msg__quote-text">{{ quoteText(item.msg.replyTo) }}</span>
                </div>

                <!-- 撤回：满 3 分钟后整条从列表中过滤掉 -->
                <div v-if="item.msg.isRecalled" class="mc-msg__bubble recalled">
                  {{ recallLabel(item.msg) }}
                </div>

                <!-- 图片：点击放大 -->
                <div v-else-if="item.msg.msgType === 'image'" class="mc-msg__media">
                  <!-- 不能用 loading="lazy"：DynamicScroller 依赖测量 item 高度，
                       图片未加载时高度为 0 → 永远进不了视口 → 永远不会加载（死循环）。
                       虚拟滚动本身已经保证只渲染视口附近的消息，无需再 lazy。 -->
                  <img
                    :src="item.msg.content"
                    alt=""
                    decoding="async"
                    @click="onMediaClick(item.msg, $event)"
                    @contextmenu.prevent.stop="openImageMenu($event, item.msg)"
                  />
                </div>

                <!-- 语音：自定义气泡（宽度随时长、进度条、同时只播一条） -->
                <VoiceMessage
                  v-else-if="item.msg.msgType === 'audio'"
                  :src="item.msg.content"
                  :mine="senderOf(item.msg)"
                />

                <!-- 视频：原生控件（播放/进度/全屏交给浏览器）。
                     固定 16:9 占位：DynamicScroller 依赖测量高度，
                     元数据到达前高度为 0 会让列表跳动。 -->
                <div v-else-if="item.msg.msgType === 'video'" class="mc-msg__video">
                  <video
                    :src="item.msg.content"
                    controls
                    playsinline
                    preload="metadata"
                    @contextmenu.prevent.stop="openMenu($event, item.msg)"
                  />
                </div>

                <!-- 表情：透明底、无气泡（和图片区分：表情是"贴"在对话里的） -->
                <div v-else-if="item.msg.msgType === 'sticker'" class="mc-msg__sticker">
                  <img
                    :src="item.msg.content"
                    alt=""
                    decoding="async"
                    @click="onMediaClick(item.msg, $event)"
                    @contextmenu.prevent.stop="openImageMenu($event, item.msg)"
                  />
                </div>

                <!-- 自定义类型：插件白名单渲染 -->
                <div v-else-if="item.msg.msgType !== 'text'" class="mc-msg__bubble custom">
                  <DeclarativeNodes v-if="isRenderedOk(item.msg)" :nodes="renderedNodes(item.msg)" />
                  <span v-else class="text-placeholder">
                    {{ t('message.unknownType', { type: item.msg.msgType }) }}
                  </span>
                </div>

                <!-- 普通文本 -->
                <div v-else class="mc-msg__bubble" :class="{ pending: item.msg.pending, failed: item.msg.failed }">
                  <span class="mc-msg__text">
                    <template v-for="(p, pi) in textParts(item.msg)" :key="pi">
                      <span v-if="p.at" class="mc-msg__at">{{ p.t }}</span>
                      <template v-else>{{ p.t }}</template>
                    </template>
                  </span>
                  <span
                    v-if="canRecall(item.msg)"
                    class="mc-msg__recall"
                    role="button"
                    tabindex="0"
                    :aria-label="t('message.recallAria')"
                    @click.stop="recall(item.msg)"
                    @keydown.enter="recall(item.msg)"
                  >{{ t('message.recall') }}</span>
                  <span v-if="item.msg.failed" class="mc-msg__resend" @click.stop="convStore.resend(item.msg)">
                    {{ t('message.resend') }}
                  </span>
                </div>

                <!-- 表情回应：已有点击切换，+ 展开预设表情。
                     暂未开放（FEATURES.reactions=false 隐藏，逻辑保留），开关在 src/config/features.ts -->
                <div
                  v-if="FEATURES.reactions && !item.msg.isRecalled && item.msg.id > 0"
                  class="mc-msg__reactions"
                >
                  <button
                    v-for="r in item.msg.reactions ?? []"
                    :key="r.emoji"
                    type="button"
                    class="mc-msg__reaction"
                    :class="{ 'is-mine': r.mine }"
                    @click="react(item.msg, r.emoji)"
                  >
                    {{ r.emoji }}<span class="num">{{ r.count }}</span>
                  </button>
                  <button
                    type="button"
                    class="mc-msg__reaction-add"
                    :aria-label="t('message.addReaction')"
                    :title="t('message.addReaction')"
                    @click="toggleReactionPicker(item.msg.id)"
                  >
                    +
                  </button>
                  <div v-if="reactionPicker === item.msg.id" class="mc-msg__reaction-picker">
                    <button
                      v-for="e in REACTION_EMOJIS"
                      :key="e"
                      type="button"
                      @click="react(item.msg, e)"
                    >
                      {{ e }}
                    </button>
                  </div>
                </div>

                <div class="mc-msg__meta text-placeholder num">
                  <!-- 发送失败：所有会话类型都给重试入口（原来只有单聊显示了文字，群里连提示都没有） -->
                  <button
                    v-if="item.msg.failed"
                    type="button"
                    class="mc-msg__retry"
                    :aria-label="t('message.resend')"
                    @click.stop="retrySend(item.msg)"
                  >
                    {{ t('message.sendFailed') }} · {{ t('message.resend') }}
                  </button>
                  <template v-else-if="senderOf(item.msg) && conv?.type === 1">
                    {{ messageStatus(item.msg) }} ·
                  </template>
                  {{ fmtTime(item.msg.createdAt) }}
                  <span v-if="item.msg.editedAt"> · {{ t('message.edited') }}</span>
                </div>
              </div>
            </div>
          </template>
        </DynamicScrollerItem>

        </template>
    </DynamicScroller>

    <!-- 新会话 / 空会话：给一个明确的开始提示，而不是一片空白 -->
    <div v-else-if="conv" class="mc-msgpane__empty">
      <span class="mc-msgpane__empty-icon" aria-hidden="true"><ChatIcon /></span>
      <p>{{ t('message.emptyChat') }}</p>
    </div>

    <div v-if="typingNames.length" class="mc-msgpane__typing text-placeholder">
      {{ t('message.typingOnly', { names: typingNames.join(', ') }) }}
    </div>

    <MessageComposer v-if="conv" />

    <!-- 图片放大：挂在虚拟列表之外（放在列表插槽里会按可见项渲染多份） -->
    <Teleport to="body">
      <Transition name="mc-lb">
        <div v-if="previewSrc" class="mc-lightbox" @click="previewSrc = ''">
          <img :src="previewSrc" alt="" />
        </div>
      </Transition>
    </Teleport>

    <!-- 消息右键菜单 -->
    <ContextMenu
      v-model:visible="menuVisible"
      :x="menuX"
      :y="menuY"
      :items="menuItems"
      @select="onMenuSelect"
    />

    <!-- 转发 / 合并转发：选择目标会话 -->
    <Teleport to="body">
      <div
        v-if="forwardSource || mergeSourceIds"
        class="mc-forward"
        @click.self="closeForwardPicker"
      >
        <div
          ref="forwardPanelRef"
          class="mc-forward__panel"
          role="dialog"
          aria-modal="true"
          tabindex="-1"
        >
          <div class="mc-forward__head">
            <span>{{ mergeSourceIds ? t('message.mergeForward') : t('message.forwardTitle') }}</span>
            <button
              type="button"
              class="mc-forward__close"
              :aria-label="t('common.cancel')"
              @click="closeForwardPicker"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
              </svg>
            </button>
          </div>
          <div class="mc-forward__list">
            <button
              v-for="c in forwardTargets"
              :key="c.id"
              type="button"
              class="mc-forward__item"
              :class="{ 'is-disabled': c.id === forwardDisabledId }"
              :disabled="c.id === forwardDisabledId"
              @click="pickForwardTarget(c.id)"
            >
              <span class="mc-forward__name">{{ convLabel(c) }}</span>
              <span v-if="c.id === forwardDisabledId" class="mc-forward__hint">
                {{ t('message.forwardSameConv') }}
              </span>
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 举报消息：填写理由后提交 -->
    <Teleport to="body">
      <div v-if="reportTarget" class="mc-forward" @click.self="reportTarget = null">
        <div class="mc-forward__panel" role="dialog" aria-modal="true">
          <div class="mc-forward__head">
            <span>{{ t('message.reportTitle') }}</span>
            <button
              type="button"
              class="mc-forward__close"
              :aria-label="t('common.cancel')"
              @click="reportTarget = null"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
              </svg>
            </button>
          </div>
          <div class="mc-report__body">
            <textarea
              v-model="reportReason"
              class="mc-report__input"
              :maxlength="200"
              rows="4"
              :placeholder="t('message.reportPlaceholder')"
            />
            <div class="mc-report__foot">
              <UiButton variant="outline" size="small" @click="reportTarget = null">
                {{ t('common.cancel') }}
              </UiButton>
              <UiButton theme="danger" size="small" :loading="reporting" @click="submitReport">
                {{ t('message.reportSubmit') }}
              </UiButton>
            </div>
          </div>
        </div>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.mc-msgpane {
  position: relative;
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  /* 透出 body 上的全站背景（颜色 + 纹理） */
  background: transparent;
}

.mc-msgpane__header {
  position: relative;
  height: 56px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  padding: 0 20px;
  border-bottom: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
}

.mc-msgpane__identity {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.mc-msgpane__title {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
  font-weight: 600;
  font-size: 14px;
  line-height: 1.25;
}

.mc-msgpane__name {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-msgpane__sub {
  font-size: var(--mc-fs-sm);
  font-weight: 400;
  line-height: 1.3;
}

/* 返回按钮：仅窄屏（单面板模式）显示 */
.mc-msgpane__back {
  display: none;
  margin-right: 4px;
}

/* 背景入口：靠右，低干扰图标按钮 */
@media (max-width: 768px) {
  .mc-msgpane__back {
    display: inline-flex;
  }
}

.mc-msgpane__list {
  flex: 1;
  min-height: 0;
  padding: 16px 20px;
}

.mc-msgpane__loading {
  text-align: center;
  padding: 8px 0;
  font-size: 12px;
}

/* 日期分隔：文字居中 + 两侧细线，和消息流拉开层次 */
.mc-msgpane__date {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px 0 6px;
  font-size: 12px;
  color: var(--mc-text-faint);
}

.mc-msgpane__date::before,
.mc-msgpane__date::after {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--mc-hairline);
}

.mc-msg {
  display: flex;
  gap: 10px;
  /* 气泡之间的呼吸感：上下各 11px。
     用 padding 而非 margin：DynamicScroller 按 wrapper 高度测量 item，
     margin 不计入会导致相邻消息的 meta 行互相贴边、视觉上被裁。 */
  padding: 11px 0;
}

.mc-msg.mine {
  flex-direction: row-reverse;
}

.mc-msg__body {
  max-width: 64%;
  display: flex;
  flex-direction: column;
}

.mc-msg.mine .mc-msg__body {
  align-items: flex-end;
}

.mc-msg__sender {
  font-size: 12px;
  margin-bottom: 2px;
}

/* ===== 引用块 ===== */
.mc-msg__quote {
  display: flex;
  flex-direction: column;
  gap: 1px;
  max-width: 100%;
  margin-bottom: 4px;
  padding: 5px 9px;
  border-left: 2px solid var(--td-brand-color);
  border-radius: 4px;
  background: var(--td-bg-color-secondarycontainer);
  cursor: pointer;
  transition: background-color var(--mc-dur-instant) var(--mc-ease-out);
}

.mc-msg__quote:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-msg__quote-name {
  font-size: var(--mc-fs-caption);
  color: var(--td-brand-color);
}

.mc-msg__quote-text {
  font-size: var(--mc-fs-sm);
  color: var(--td-text-color-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 跳转后的短暂高亮 */
.mc-msg.is-flash {
  animation: mc-msg-flash 1.2s var(--mc-ease-out);
}

@keyframes mc-msg-flash {
  0%,
  40% {
    background: color-mix(in srgb, var(--td-brand-color) 14%, transparent);
    border-radius: var(--mc-radius-md);
  }
  100% {
    background: transparent;
  }
}

@media (prefers-reduced-motion: reduce) {
  .mc-msg.is-flash {
    animation: none;
  }
}

/* ===== 图片 / 语音消息 ===== */
.mc-msg__media {
  max-width: 240px;
  /* 加载完成前先占一块高度，避免图片到达时消息列表整体跳动 */
  min-height: 80px;
  border-radius: var(--mc-radius-lg);
  overflow: hidden;
  background: var(--td-bg-color-container);
  line-height: 0;
}

.mc-msg__media img {
  display: block;
  width: 100%;
  max-height: 280px;
  object-fit: cover;
  cursor: zoom-in;
}

/* 视频：固定 16:9 占位，竖屏视频以黑边居中（object-fit: contain） */
.mc-msg__video {
  width: min(260px, 62vw);
  aspect-ratio: 16 / 9;
  border-radius: var(--mc-radius-lg);
  overflow: hidden;
  background: #000;
  line-height: 0;
}

.mc-msg__video video {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
}

/* 手机：竖屏视频按视口宽度走，别在窄屏里只占 62% */
@media (max-width: 768px) {
  .mc-msg__video {
    width: min(70vw, 260px);
  }
}

/* 发送失败的重发入口：低调但可点，不用另起一行 */
.mc-msg__retry {
  padding: 0;
  border: none;
  background: transparent;
  color: var(--td-error-color);
  font: inherit;
  cursor: pointer;
  text-decoration: underline;
  text-underline-offset: 2px;
}

.mc-msg__retry:hover {
  color: var(--td-error-color);
  text-decoration-thickness: 2px;
}

/* 表情：不套气泡背景，透明贴图 */
.mc-msg__sticker img {
  display: block;
  width: 120px;
  height: 120px;
  object-fit: contain;
  cursor: zoom-in;
}

.mc-msg__voice audio {
  height: 34px;
  max-width: 240px;
  border-radius: var(--mc-radius-md);
}

.mc-lightbox {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 32px;
  background: rgba(0, 0, 0, 0.82);
  cursor: zoom-out;
}

.mc-lightbox img {
  max-width: 100%;
  max-height: 100%;
  border-radius: var(--mc-radius-lg);
  box-shadow: 0 24px 64px -24px rgba(0, 0, 0, 0.6);
}

.mc-lb-enter-active,
.mc-lb-leave-active {
  transition: opacity 160ms var(--mc-ease-out);
}

.mc-lb-enter-from,
.mc-lb-leave-to {
  opacity: 0;
}

.mc-msg__bubble {
  position: relative;
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  padding: 8px 12px;
  word-break: break-word;
  white-space: pre-wrap;
}

/* 气泡朝头像方向收一个角，弱化方向感，不额外画尾巴 */
.mc-msg:not(.mine) .mc-msg__bubble {
  border-top-left-radius: 4px;
}

.mc-msg.mine .mc-msg__bubble {
  background: var(--td-brand-color);
  border-color: transparent;
  border-top-right-radius: 4px;
  color: #fff;
}

.mc-msg__bubble.pending {
  opacity: 0.55;
}

.mc-msg__bubble.failed {
  outline: 1px solid var(--td-error-color);
}

.mc-msg__bubble.recalled {
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  background: transparent;
  border-color: transparent;
  box-shadow: none;
  padding: 2px 0;
}

/* ===== 多选 / 合并转发 ===== */
.mc-selectbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 20px;
  border-bottom: 1px solid var(--mc-hairline);
  background: var(--td-bg-color-container);
}

.mc-selectbar__count {
  flex: 1;
  font-size: 13px;
  color: var(--td-text-color-secondary);
}

.mc-msg.is-selecting {
  padding-left: 6px;
  padding-right: 6px;
  border-radius: var(--mc-radius-sm);
  cursor: pointer;
}

.mc-msg.is-selected {
  background: color-mix(in srgb, var(--td-brand-color) 10%, transparent);
}

.mc-msg__check {
  flex-shrink: 0;
  align-self: center;
  width: 18px;
  height: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--td-component-border);
  border-radius: 50%;
  background: var(--td-bg-color-container);
  color: transparent;
  font-size: 12px;
  line-height: 1;
}

.mc-msg__check.is-on {
  border-color: var(--td-brand-color);
  background: var(--td-brand-color);
  color: #fff;
}

/* ===== 群公告 ===== */
.mc-announce {
  margin: 8px 12px 0;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  background: color-mix(in srgb, var(--td-brand-color) 8%, transparent);
  overflow: hidden;
}

.mc-announce__head {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 7px 10px;
  border: none;
  background: transparent;
  font: inherit;
  font-size: var(--mc-fs-sm);
  color: var(--td-text-color-primary);
  text-align: left;
  cursor: pointer;
}

.mc-announce__head:hover {
  background: color-mix(in srgb, var(--td-brand-color) 6%, transparent);
}

.mc-announce__head > svg {
  flex-shrink: 0;
  color: var(--td-brand-color);
}

.mc-announce__label {
  flex-shrink: 0;
  color: var(--td-brand-color);
  font-weight: 500;
}

.mc-announce__preview {
  flex: 1;
  min-width: 0;
  color: var(--td-text-color-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-announce__toggle {
  flex-shrink: 0;
  font-size: var(--mc-fs-caption);
  color: var(--td-text-color-placeholder);
}

.mc-announce__body {
  padding: 0 10px 9px;
}

.mc-announce__text {
  margin: 0;
  font-size: var(--mc-fs-body);
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-word;
  user-select: text;
}

.mc-announce__meta {
  margin: 6px 0 0;
  font-size: var(--mc-fs-caption);
}

/* ===== 转发：气泡上的来源标注 ===== */
.mc-msg__forward {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  margin-bottom: 2px;
  font-size: var(--mc-fs-caption);
  color: var(--td-text-color-placeholder);
}

/* ===== 转发：选择目标会话 ===== */
.mc-forward {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(0, 0, 0, 0.45);
}

.mc-forward__panel {
  width: 320px;
  max-height: 70vh;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  background: var(--td-bg-color-container);
  box-shadow: var(--mc-elev-3);
  overflow: hidden;
}

.mc-forward__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px;
  border-bottom: 1px solid var(--mc-hairline);
  font-size: var(--mc-fs-emphasis);
  font-weight: 600;
}

.mc-forward__close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  color: var(--td-text-color-secondary);
  cursor: pointer;
}

.mc-forward__close:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-forward__list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 6px;
}

.mc-forward__item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  padding: 9px 10px;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  font: inherit;
  font-size: var(--mc-fs-body);
  color: var(--td-text-color-primary);
  text-align: left;
  cursor: pointer;
  transition: background-color var(--mc-dur-instant) var(--mc-ease-out);
}

.mc-forward__item:hover:not(.is-disabled) {
  background: var(--td-bg-color-container-hover);
}

.mc-forward__item.is-disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.mc-forward__name {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-forward__hint {
  flex-shrink: 0;
  font-size: var(--mc-fs-caption);
  color: var(--td-text-color-placeholder);
}

/* ===== 举报消息 ===== */
.mc-report__body {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px 14px 14px;
}

.mc-report__input {
  width: 100%;
  min-height: 88px;
  resize: vertical;
  padding: 8px 10px;
  border: 1px solid var(--td-component-border);
  border-radius: var(--mc-radius-sm);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  font: inherit;
  font-size: 13px;
  line-height: 1.5;
  outline: none;
}

.mc-report__input:focus {
  border-color: var(--td-brand-color);
}

.mc-report__foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

/* 必须盖过 .mc-msg.mine .mc-msg__bubble（3 类选择器）：
   否则自己撤回的提示会被染成品牌蓝底白字，看起来像一条真消息。 */
.mc-msg.is-recalled .mc-msg__bubble.recalled {
  background: transparent;
  color: var(--td-text-color-placeholder);
  border-color: transparent;
  box-shadow: none;
}

/* 撤回提示是一行系统文案：居中、不带头像、不带已读/时间戳。
   padding 而不是 margin：虚拟列表里相邻两条的 margin 会折叠，
   实测会变成 0 间隙、两行提示看起来像重影。 */
.mc-msg.is-recalled {
  justify-content: center;
  padding: 5px 0;
}

.mc-msg.is-recalled .mc-msg__body {
  max-width: 100%;
  align-items: center;
}

.mc-msg.is-recalled .mc-msg__meta {
  display: none;
}

.mc-msg__bubble.custom {
  white-space: normal;
}

.mc-msg__text {
  user-select: text;
}

/* 被 @ 的昵称：微信里是蓝色，这里跟随气泡前景色加粗，亮暗都可读 */
.mc-msg__at {
  font-weight: 600;
  color: var(--td-brand-color);
}

.mc-msg.mine .mc-msg__at {
  color: #fff;
  text-decoration: underline;
  text-underline-offset: 2px;
}

.mc-msg__recall,
.mc-msg__resend {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s;
  user-select: none;
  white-space: nowrap;
}

.mc-msg__recall {
  right: -46px;
}

.mc-msg.mine .mc-msg__recall {
  left: -46px;
  right: auto;
}

.mc-msg:hover .mc-msg__recall,
.mc-msg:focus-within .mc-msg__recall {
  opacity: 1;
}

.mc-msg__recall:focus-visible {
  opacity: 1;
  outline: 1px solid var(--td-brand-color);
  border-radius: 2px;
}

.mc-msg__resend {
  right: -52px;
  color: var(--td-error-color);
  opacity: 1;
}

/* ===== 表情回应 ===== */
.mc-msg__reactions {
  position: relative;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  margin-top: 4px;
}

.mc-msg__reaction,
.mc-msg__reaction-add {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 1px 7px;
  border: 1px solid var(--mc-hairline);
  border-radius: 999px;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-primary);
  font: inherit;
  font-size: 12px;
  line-height: 18px;
  cursor: pointer;
  transition:
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    background-color var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-msg__reaction:hover,
.mc-msg__reaction.is-mine {
  border-color: var(--td-brand-color);
}

.mc-msg__reaction.is-mine {
  background: var(--td-brand-color-1, rgba(0, 82, 217, 0.08));
  color: var(--td-brand-color);
}

.mc-msg__reaction .num {
  font-size: 11px;
  color: var(--td-text-color-secondary);
}

.mc-msg__reaction.is-mine .num {
  color: var(--td-brand-color);
}

.mc-msg__reaction-add {
  width: 22px;
  height: 22px;
  padding: 0;
  justify-content: center;
  border-style: dashed;
  color: var(--td-text-color-placeholder);
  opacity: 0;
}

.mc-msg:hover .mc-msg__reaction-add,
.mc-msg:focus-within .mc-msg__reaction-add,
.mc-msg__reaction-add:focus-visible {
  opacity: 1;
}

@media (hover: none) {
  .mc-msg__reaction-add {
    opacity: 1;
  }
}

.mc-msg__reaction-picker {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 0;
  z-index: 20;
  display: flex;
  gap: 2px;
  padding: 4px;
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-md);
  background: var(--td-bg-color-container);
  box-shadow: var(--mc-shadow-2);
}

.mc-msg.mine .mc-msg__reaction-picker {
  left: auto;
  right: 0;
}

.mc-msg__reaction-picker button {
  width: 30px;
  height: 30px;
  padding: 0;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
}

.mc-msg__reaction-picker button:hover {
  background: var(--td-bg-color-container-hover);
}

.mc-msg__meta {
  font-size: var(--mc-fs-sm);
  /* 明确行高：12px 字体在默认行高下中文字形下缘容易被裁 */
  line-height: 18px;
  margin-top: 4px;
  user-select: none;
  letter-spacing: 0.01em;
}

/* 新会话空态：图标 + 一句话，不铺满整块背景 */
.mc-msgpane__empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 24px;
  text-align: center;
  color: var(--mc-text-faint);
}

.mc-msgpane__empty-icon {
  width: 48px;
  height: 48px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);
}

.mc-msgpane__empty p {
  margin: 0;
  font-size: 13px;
  line-height: 1.6;
}

.mc-msgpane__typing {
  height: 22px;
  flex-shrink: 0;
  padding: 0 20px;
  font-size: 12px;
}
</style>
