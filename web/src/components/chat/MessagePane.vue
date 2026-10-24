<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { MessagePlugin, Button as TButton } from 'tdesign-vue-next'
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller'
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css'
import { ArrowLeftIcon } from 'tdesign-icons-vue-next'

import AvatarBadge from './AvatarBadge.vue'
import MessageComposer from './MessageComposer.vue'
import DeclarativeNodes from '@/components/declarative'
import { usePluginHost } from '@/composables/usePluginHost'
import { useAppI18n } from '@/composables/useAppI18n'
import { convApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useWsStore } from '@/stores/ws'
import type { Message, UIDescNode } from '@/types/proto'

const auth = useAuthStore()
const convStore = useConversationsStore()
const ws = useWsStore()
const host = usePluginHost()
const { t } = useAppI18n()

const scrollerRef = ref<{ $el: HTMLElement } | null>(null)
const stickToBottom = ref(true)

const conv = computed(() => convStore.currentConv)
const messages = computed(() => convStore.currentMessages)

interface DisplayItem {
  key: string
  kind: 'date' | 'msg'
  label?: string
  msg?: Message
  /** 同一发送者连续消息的第一条：显示头像与昵称 */
  firstInGroup?: boolean
  /** 连续消息的最后一条：显示时间等元信息 */
  lastInGroup?: boolean
}

/** 同一发送者、间隔 5 分钟内的连续消息归为一组，减少重复头像与时间行 */
const GROUP_GAP = 5 * 60 * 1000

const displayItems = computed<DisplayItem[]>(() => {
  const out: DisplayItem[] = []
  let lastDate = ''
  let prev: Message | undefined
  for (const m of messages.value) {
    const d = new Date(m.createdAt).toDateString()
    if (d !== lastDate) {
      const prevItem = out[out.length - 1]
      if (prevItem?.kind === 'msg') prevItem.lastInGroup = true
      lastDate = d
      prev = undefined
      out.push({ key: `date-${d}`, kind: 'date', label: formatDateLabel(m.createdAt) })
    }
    const now = new Date(m.createdAt).getTime()
    const first =
      !prev || prev.senderId !== m.senderId || now - new Date(prev.createdAt).getTime() > GROUP_GAP
    if (first) {
      const prevItem = out[out.length - 1]
      if (prevItem?.kind === 'msg') prevItem.lastInGroup = true
    }
    out.push({
      key: m.clientMsgId || `msg-${m.id}`,
      kind: 'msg',
      msg: m,
      firstInGroup: first,
      lastInGroup: false,
    })
    prev = m
  }
  const lastItem = out[out.length - 1]
  if (lastItem?.kind === 'msg') lastItem.lastInGroup = true
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

/** 新到达消息的一次性入场 key，避免虚拟滚动重新挂载时反复播放动画 */
const animatedKey = ref('')
let animateTimer: ReturnType<typeof setTimeout> | null = null

/** 只观察最后一条消息的 key：追加新消息时变化，向上翻历史时不变 */
const lastMessageKey = computed(() => {
  const last = messages.value[messages.value.length - 1]
  return last ? last.clientMsgId || `msg-${last.id}` : ''
})

watch(lastMessageKey, (key, prevKey) => {
  if (key && prevKey !== undefined && key !== prevKey) {
    animatedKey.value = key
    if (animateTimer) clearTimeout(animateTimer)
    animateTimer = setTimeout(() => {
      animatedKey.value = ''
    }, 460)
  }
  if (stickToBottom.value) scrollToBottom()
})

onBeforeUnmount(() => {
  if (animateTimer) clearTimeout(animateTimer)
})

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

// ===== 群成员资料缓存（昵称 / 头像 / 取色） =====

interface MemberProfile {
  name: string
  avatar: string
  color: string
}

const memberCache = reactive<Record<number, MemberProfile>>({})

function memberProfile(userId: number): MemberProfile {
  const hit = memberCache[userId]
  if (hit) return hit
  void loadMemberProfiles()
  return { name: t('message.userFallback', { n: userId }), avatar: '', color: '#7b8290' }
}

function memberName(userId: number): string {
  return memberProfile(userId).name
}

let memberProfilesLoading = false

async function loadMemberProfiles() {
  if (memberProfilesLoading || !conv.value || conv.value.type !== 2) return
  memberProfilesLoading = true
  try {
    const members = await convApi.members(conv.value.id)
    for (const m of members) {
      memberCache[m.userId] = {
        name: m.user.nickname,
        avatar: m.user.avatar ?? '',
        color: m.user.avatarColor || '#7b8290',
      }
    }
  } catch {
    /* 忽略：下次渲染重试 */
  } finally {
    memberProfilesLoading = false
  }
}

// 切换会话时清空昵称缓存，避免把上一个群的成员名带过来
watch(
  () => conv.value?.id,
  () => {
    for (const k of Object.keys(memberCache)) delete memberCache[Number(k)]
  },
)

// ===== 撤回 =====

function canRecall(m: Message): boolean {
  if (m.isRecalled || m.senderId !== auth.user?.id || m.id <= 0) return false
  return Date.now() - new Date(m.createdAt).getTime() < 2 * 60 * 1000
}

function recall(m: Message) {
  convStore.recallMessage(m.id)
  void MessagePlugin.success(t('message.recallOk'))
}

// ===== 已读回执（单聊） =====

const peerReadUpTo = computed(() => {
  const c = conv.value
  if (!c || c.type !== 1 || !c.peer) return 0
  return convStore.readByConv[c.id]?.[c.peer.id] ?? 0
})

/** 自己发出的最后一条已落库消息：只在这一条上展示已读/未读，避免每条都重复 */
const lastOwnMessageId = computed(() => {
  const uid = auth.user?.id
  if (!uid) return 0
  for (let i = messages.value.length - 1; i >= 0; i--) {
    const m = messages.value[i]
    if (m && m.senderId === uid && m.id > 0) return m.id
  }
  return 0
})

function showReadState(m: Message): boolean {
  return conv.value?.type === 1 && senderOf(m) && m.id > 0 && m.id === lastOwnMessageId.value
}

/** 连续消息只在最后一条展示时间；发送失败的消息始终展示，保证「重发」可达 */
function showMeta(item: DisplayItem): boolean {
  return !!item.lastInGroup || !!item.msg?.failed
}

// ===== 顶部会话信息 =====

const headerAvatar = computed(() => {
  const c = conv.value
  if (!c) return null
  return c.type === 1
    ? {
        name: c.peer?.nickname ?? '?',
        src: c.peer?.avatar ?? '',
        color: c.peer?.avatarColor ?? '#0052d9',
      }
    : { name: c.name ?? '#', src: '', color: '#2ba471' }
})

const peerOnline = computed(() => {
  const p = conv.value?.peer
  return !!p && ws.onlineUsers.has(p.id)
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
  const c = conv.value
  if (c?.type === 1 && c.peer?.id === m.senderId) return c.peer.avatarColor
  if (c?.type === 2) return memberProfile(m.senderId).color
  return '#7b8290'
}

/** 发送者头像：单聊取 peer，群聊取成员资料缓存 */
const senderAvatar = (m: Message) => {
  const c = conv.value
  if (c?.type === 1 && c.peer?.id === m.senderId) return c.peer.avatar ?? ''
  if (c?.type === 2) return memberProfile(m.senderId).avatar
  return ''
}
</script>

<template>
  <section class="mc-msgpane">
    <header class="mc-msgpane__header">
      <TButton
        class="mc-msgpane__back"
        variant="text"
        shape="square"
        :aria-label="t('message.backToConversations')"
        @click="convStore.currentConvId = null"
      >
        <template #icon><ArrowLeftIcon /></template>
      </TButton>
      <template v-if="conv">
        <AvatarBadge
          v-if="headerAvatar"
          class="mc-msgpane__avatar"
          :name="headerAvatar.name"
          :src="headerAvatar.src"
          :color="headerAvatar.color"
          :size="34"
        />
        <div class="mc-msgpane__heading">
          <div class="mc-msgpane__title" :title="conv.type === 1 ? (conv.peer?.nickname ?? '') : (conv.name ?? '')">
            {{ conv.type === 1 ? conv.peer?.nickname : conv.name }}
          </div>
          <div class="mc-msgpane__sub text-placeholder">
            <span
              v-if="conv.type === 1"
              class="mc-msgpane__presence"
              :class="{ 'is-online': peerOnline }"
            />
            {{
              conv.type === 1
                ? peerOnline
                  ? t('chat.online')
                  : t('chat.offline')
                : t('chat.membersCount', { n: conv.memberCount })
            }}
          </div>
        </div>
      </template>
    </header>

    <div v-if="conv" class="mc-msgpane__stream">
      <DynamicScroller
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
              <div class="mc-msgpane__date">
                <span class="text-placeholder">{{ item.label }}</span>
              </div>
            </template>
            <template v-else-if="item.msg">
              <div
                class="mc-msg"
                :class="{
                  mine: senderOf(item.msg),
                  repeat: !item.firstInGroup && !senderOf(item.msg),
                  'group-start': item.firstInGroup,
                  'is-new': !!animatedKey && item.key === animatedKey,
                }"
              >
                <AvatarBadge
                  v-if="!senderOf(item.msg) && item.firstInGroup"
                  :name="conv?.type === 1 ? conv.peer?.nickname ?? '?' : memberName(item.msg.senderId)"
                  :src="senderAvatar(item.msg)"
                  :color="senderAvatarColor(item.msg)"
                  :size="32"
                />
                <div class="mc-msg__body">
                  <div
                    v-if="conv?.type === 2 && !senderOf(item.msg) && item.firstInGroup"
                    class="mc-msg__sender"
                  >
                    {{ memberName(item.msg.senderId) }}
                  </div>

                  <!-- 撤回 -->
                  <div v-if="item.msg.isRecalled" class="mc-msg__bubble recalled">{{ t('message.recalled') }}</div>

                  <!-- 自定义类型：插件白名单渲染 -->
                  <div v-else-if="item.msg.msgType !== 'text'" class="mc-msg__bubble custom">
                    <DeclarativeNodes v-if="isRenderedOk(item.msg)" :nodes="renderedNodes(item.msg)" />
                    <span v-else class="text-placeholder">
                      {{ t('message.unknownType', { type: item.msg.msgType }) }}
                    </span>
                  </div>

                  <!-- 普通文本 -->
                  <div v-else class="mc-msg__bubble" :class="{ pending: item.msg.pending, failed: item.msg.failed }">
                    <span class="mc-msg__text">{{ item.msg.content }}</span>
                  </div>

                  <div v-if="showMeta(item)" class="mc-msg__meta text-placeholder num">
                    <button
                      v-if="canRecall(item.msg)"
                      type="button"
                      class="mc-msg__action"
                      :aria-label="t('message.recallAria')"
                      @click="recall(item.msg)"
                    >
                      {{ t('message.recall') }}
                    </button>
                    <button
                      v-if="item.msg.failed"
                      type="button"
                      class="mc-msg__action is-danger"
                      @click="convStore.resend(item.msg)"
                    >
                      {{ t('message.resend') }}
                    </button>
                    <span v-if="showReadState(item.msg)">
                      {{ peerReadUpTo >= item.msg.id ? t('message.read') : t('message.unread') }} ·
                    </span>
                    <span>{{ fmtTime(item.msg.createdAt) }}</span>
                  </div>
                </div>
              </div>
            </template>
          </DynamicScrollerItem>
        </template>
      </DynamicScroller>

      <Transition name="mc-fade-scale">
        <button
          v-if="!stickToBottom"
          type="button"
          class="mc-msgpane__jump"
          :aria-label="t('chat.scrollToLatest')"
          :title="t('chat.scrollToLatest')"
          @click="scrollToBottom"
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path
              d="M12 5v12m0 0 5-5m-5 5-5-5"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </button>
      </Transition>
    </div>

    <div class="mc-msgpane__typing" aria-live="polite">
      <span v-if="typingNames.length" class="mc-typing text-placeholder">
        {{ t('message.typingOnly', { names: typingNames.join(', ') }) }}
        <span class="mc-typing__dots" aria-hidden="true"><i /><i /><i /></span>
      </span>
    </div>

    <MessageComposer v-if="conv" />
  </section>
</template>

<style scoped>
.mc-msgpane {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: var(--td-bg-color-page);
}

.mc-msgpane__header {
  height: 60px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 20px;
  border-bottom: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
}

.mc-msgpane__avatar {
  flex-shrink: 0;
}

.mc-msgpane__heading {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.mc-msgpane__title {
  font-weight: 600;
  font-size: 15px;
  line-height: 1.3;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-msgpane__sub {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  font-weight: 400;
  line-height: 1.3;
}

/* 在线状态点：只作为状态提示，不抢标题层级 */
.mc-msgpane__presence {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--td-text-color-placeholder);
}

.mc-msgpane__presence.is-online {
  background: var(--td-success-color, #2ba471);
  animation: mc-soft-pulse 2.4s ease-in-out infinite;
}

/* 返回按钮：仅窄屏（单面板模式）显示 */
.mc-msgpane__back {
  display: none;
  margin-right: 2px;
}

@media (max-width: 768px) {
  .mc-msgpane__back {
    display: inline-flex;
  }
}

.mc-msgpane__stream {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.mc-msgpane__list {
  flex: 1;
  min-height: 0;
  padding: 8px 24px 12px;
}

.mc-msgpane__loading {
  text-align: center;
  padding: 8px 0;
  font-size: 12px;
}

/* 日期分隔：细线夹住日期，避免居中孤立的一行字 */
.mc-msgpane__date {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 18px 0 10px;
  font-size: 11px;
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
  align-items: flex-start;
  gap: 8px;
}

/* 连续消息缩短行距，换人时留出呼吸 */
.mc-msg.group-start {
  margin-top: 12px;
}

/* 同人连续消息不重复头像，用 40px 占位保持气泡对齐 */
.mc-msg.repeat {
  padding-left: 40px;
}

/* 新到达的消息：只播放一次轻微上浮 */
.mc-msg.is-new {
  animation: mc-bubble-in var(--mc-dur-slow) var(--mc-ease-out) both;
}

.mc-msg.mine {
  flex-direction: row-reverse;
}

.mc-msg__body {
  max-width: 64%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.mc-msg.mine .mc-msg__body {
  align-items: flex-end;
}

.mc-msg__sender {
  font-size: 12px;
  font-weight: 500;
  color: var(--td-text-color-secondary);
  margin: 0 0 3px 2px;
}

.mc-msg__bubble {
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  border-radius: 12px;
  border-bottom-left-radius: 4px;
  padding: 8px 12px;
  font-size: 14px;
  line-height: 1.55;
  word-break: break-word;
  white-space: pre-wrap;
}

.mc-msg.mine .mc-msg__bubble {
  background: var(--td-brand-color);
  border-color: transparent;
  border-bottom-left-radius: 12px;
  border-bottom-right-radius: 4px;
  color: #fff;
}

.mc-msg__bubble.pending {
  opacity: 0.55;
}

.mc-msg__bubble.failed {
  border-color: var(--td-error-color);
}

.mc-msg__bubble.recalled {
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  background: transparent;
  border-color: transparent;
  border-radius: 0;
  padding: 2px 0;
}

.mc-msg__bubble.custom {
  white-space: normal;
}

.mc-msg__text {
  user-select: text;
}

/* 时间与操作合并到一条元信息行，悬停时才出现撤回 */
.mc-msg__meta {
  display: flex;
  align-items: center;
  gap: 6px;
  min-height: 16px;
  margin-top: 3px;
  padding: 0 2px;
  font-size: 11px;
  user-select: none;
}

.mc-msg.mine .mc-msg__meta {
  justify-content: flex-end;
}

.mc-msg__action {
  padding: 0;
  border: none;
  background: transparent;
  color: var(--td-text-color-placeholder);
  font: inherit;
  font-size: 11px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s ease-out;
}

.mc-msg:hover .mc-msg__action,
.mc-msg:focus-within .mc-msg__action {
  opacity: 1;
}

.mc-msg__action:hover {
  color: var(--td-text-color-primary);
}

.mc-msg__action.is-danger {
  color: var(--td-error-color);
  opacity: 1;
}

.mc-msg__action:focus-visible {
  opacity: 1;
  outline: 1px solid var(--td-brand-color);
  outline-offset: 1px;
  border-radius: 2px;
}

/* 回到底部：浮在消息流右下角，不遮挡输入区 */
.mc-msgpane__jump {
  position: absolute;
  right: 24px;
  bottom: 14px;
  width: 32px;
  height: 32px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: 1px solid var(--td-component-border);
  border-radius: 50%;
  background: var(--td-bg-color-container);
  color: var(--td-text-color-secondary);
  cursor: pointer;
  transition:
    color var(--mc-dur-fast) var(--mc-ease-out),
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    transform var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-msgpane__jump:hover {
  color: var(--td-text-color-primary);
  border-color: var(--td-brand-color);
  transform: translateY(-1px);
}

.mc-msgpane__jump:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}

/* 固定高度：有人输入时只替换文字，不推挤消息流 */
.mc-msgpane__typing {
  height: 22px;
  flex-shrink: 0;
  padding: 0 24px;
  font-size: 12px;
  line-height: 22px;
}

.mc-typing {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  animation: mc-fade-in var(--mc-dur-fast) var(--mc-ease-out);
}

.mc-typing__dots {
  display: inline-flex;
  align-items: center;
  gap: 2px;
}

.mc-typing__dots i {
  width: 3px;
  height: 3px;
  border-radius: 50%;
  background: currentColor;
  animation: mc-dot-bounce 1.2s infinite ease-in-out;
}

.mc-typing__dots i:nth-child(2) {
  animation-delay: 0.15s;
}

.mc-typing__dots i:nth-child(3) {
  animation-delay: 0.3s;
}

@media (max-width: 768px) {
  .mc-msgpane__header {
    height: 56px;
    padding: 0 12px;
  }

  .mc-msgpane__list {
    padding: 6px 14px 10px;
  }

  .mc-msg__body {
    max-width: 74%;
  }

  .mc-msgpane__typing {
    padding: 0 14px;
  }

  .mc-msgpane__jump {
    right: 14px;
    bottom: 10px;
  }
}
</style>
