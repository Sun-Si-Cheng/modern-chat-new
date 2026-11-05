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
import { convApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFavoritesStore } from '@/stores/favorites'
import { useWsStore } from '@/stores/ws'
import type { Conversation, Message, ReplyBrief, UIDescNode } from '@/types/proto'

const auth = useAuthStore()
const convStore = useConversationsStore()
const favorites = useFavoritesStore()
const ws = useWsStore()
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
  void nextTick(() => {
    const el = scrollerRef.value?.$el as HTMLElement | undefined
    if (el) el.scrollTop = el.scrollHeight
  })
}

watch(
  () => messages.value.length,
  () => {
    if (stickToBottom.value) scrollToBottom()
  },
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

onMounted(scrollToBottom)

// ===== 群成员昵称缓存 =====

const memberNameCache = reactive<Record<number, string>>({})

function memberName(userId: number): string {
  if (memberNameCache[userId]) return memberNameCache[userId]
  void loadMemberNames()
  return t('message.userFallback', { n: userId })
}

let memberNamesLoading = false

async function loadMemberNames() {
  if (memberNamesLoading || !conv.value) return
  memberNamesLoading = true
  try {
    const members = await convApi.members(conv.value.id)
    for (const m of members) memberNameCache[m.userId] = m.user.nickname
  } catch {
    /* 忽略：下次渲染重试 */
  } finally {
    memberNamesLoading = false
  }
}

// ===== 撤回 =====

function canRecall(m: Message): boolean {
  if (m.isRecalled || m.senderId !== auth.user?.id || m.id <= 0) return false
  return Date.now() - new Date(m.createdAt).getTime() < 2 * 60 * 1000
}

function recall(m: Message) {
  convStore.recallMessage(m.id)
  void MessagePlugin.success(t('message.recallOk'))
}

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

// ===== 转发 =====

/** 待选择目标会话的消息；非 null 时弹出会话选择框 */
const forwardSource = ref<Message | null>(null)

const forwardTargets = computed(() =>
  // type=3 是系统公告会话，不能往里转发
  convStore.conversations.filter((c) => c.type !== 3),
)

function convLabel(c: Conversation): string {
  return c.name || c.peer?.nickname || t('message.forwardUnknownConv')
}

function doForward(convId: number) {
  const m = forwardSource.value
  if (!m) return
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
  clearTimeout(recallTimer)
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
    items.push({ key: 'delete', label: t('message.delete'), danger: true, dividerBefore: true })
  }
  return items
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
    return [...imageOnly, ...commonActions(m, true)]
  }

  const items: ContextMenuItem[] = []
  if (m.msgType === 'text' && m.content) {
    items.push({ key: 'copy', label: t('message.copy') })
  }
  items.push(...commonActions(m, false))
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

  if (key === 'copy') void copyMessage(m)
  if (key === 'quote') convStore.setReplyTarget(m)
  if (key === 'forward') forwardSource.value = m
  if (key === 'recall') recall(m)
  if (key === 'favorite') void toggleFavorite(m)
  if (key === 'delete') confirmDelete(m)
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

// ===== 已读回执（单聊） =====

const peerReadUpTo = computed(() => {
  const c = conv.value
  if (!c || c.type !== 1 || !c.peer) return 0
  return convStore.readByConv[c.id]?.[c.peer.id] ?? 0
})

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
          :src="conv.type === 1 ? conv.peer?.avatar : undefined"
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

    <DynamicScroller
      v-if="conv && (messages.length || convStore.loadingMessages)"
      ref="scrollerRef"
      class="mc-msgpane__list"
      :items="displayItems"
      :min-item-size="52"
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
              }"
              @contextmenu.prevent="openMenu($event, item.msg)"
            >
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
                    @click="previewSrc = item.msg.content"
                    @contextmenu.prevent.stop="openImageMenu($event, item.msg)"
                  />
                </div>

                <!-- 语音：自定义气泡（宽度随时长、进度条、同时只播一条） -->
                <VoiceMessage
                  v-else-if="item.msg.msgType === 'audio'"
                  :src="item.msg.content"
                  :mine="senderOf(item.msg)"
                />

                <!-- 表情：透明底、无气泡（和图片区分：表情是"贴"在对话里的） -->
                <div v-else-if="item.msg.msgType === 'sticker'" class="mc-msg__sticker">
                  <img
                    :src="item.msg.content"
                    alt=""
                    decoding="async"
                    @click="previewSrc = item.msg.content"
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
                    @click="recall(item.msg)"
                    @keydown.enter="recall(item.msg)"
                  >{{ t('message.recall') }}</span>
                  <span v-if="item.msg.failed" class="mc-msg__resend" @click="convStore.resend(item.msg)">
                    {{ t('message.resend') }}
                  </span>
                </div>

                <div class="mc-msg__meta text-placeholder num">
                  <template v-if="senderOf(item.msg) && conv?.type === 1">
                    {{ peerReadUpTo >= item.msg.id ? t('message.read') : t('message.unread') }} ·
                  </template>
                  {{ fmtTime(item.msg.createdAt) }}
                </div>
              </div>
            </div>
          </template>
        </DynamicScrollerItem>

        <!-- 图片放大：点任意处或 Esc 关闭 -->
        <Teleport to="body">
          <Transition name="mc-lb">
            <div v-if="previewSrc" class="mc-lightbox" @click="previewSrc = ''">
              <img :src="previewSrc" alt="" />
            </div>
          </Transition>
        </Teleport>
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

    <!-- 消息右键菜单 -->
    <ContextMenu
      v-model:visible="menuVisible"
      :x="menuX"
      :y="menuY"
      :items="menuItems"
      @select="onMenuSelect"
    />

    <!-- 转发：选择目标会话 -->
    <Teleport to="body">
      <div v-if="forwardSource" class="mc-forward" @click.self="forwardSource = null">
        <div class="mc-forward__panel" role="dialog" aria-modal="true">
          <div class="mc-forward__head">
            <span>{{ t('message.forwardTitle') }}</span>
            <button
              type="button"
              class="mc-forward__close"
              :aria-label="t('common.cancel')"
              @click="forwardSource = null"
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
              :class="{ 'is-disabled': c.id === forwardSource.conversationId }"
              :disabled="c.id === forwardSource.conversationId"
              @click="doForward(c.id)"
            >
              <span class="mc-forward__name">{{ convLabel(c) }}</span>
              <span v-if="c.id === forwardSource.conversationId" class="mc-forward__hint">
                {{ t('message.forwardSameConv') }}
              </span>
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.mc-msgpane {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  /* 透出 body 上的全站背景（颜色 + 纹理） */
  background: transparent;
}

.mc-msgpane__header {
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
