<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller'
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css'

import AvatarBadge from './AvatarBadge.vue'
import MessageComposer from './MessageComposer.vue'
import DeclarativeNodes from '@/components/declarative'
import { usePluginHost } from '@/composables/usePluginHost'
import { convApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useWsStore } from '@/stores/ws'
import type { Message, UIDescNode } from '@/types/proto'

const auth = useAuthStore()
const convStore = useConversationsStore()
const ws = useWsStore()
const host = usePluginHost()

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

const displayItems = computed<DisplayItem[]>(() => {
  const out: DisplayItem[] = []
  let lastDate = ''
  for (const m of messages.value) {
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
  if (d.toDateString() === now.toDateString()) return '今天'
  const y = new Date(now)
  y.setDate(now.getDate() - 1)
  if (d.toDateString() === y.toDateString()) return '昨天'
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
  return `用户${userId}`
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
  void MessagePlugin.success('已撤回')
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
  const c = conv.value
  if (c?.type === 1 && c.peer?.id === m.senderId) return c.peer.avatarColor
  return '#7b8290'
}
</script>

<template>
  <section class="mc-msgpane">
    <header class="mc-msgpane__header">
      <template v-if="conv">
        <div class="mc-msgpane__title">
          {{ conv.type === 1 ? conv.peer?.nickname : conv.name }}
          <span class="text-placeholder mc-msgpane__sub">
            {{ conv.type === 1 ? (conv.peer && ws.onlineUsers.has(conv.peer.id) ? '在线' : '离线') : `${conv.memberCount} 人` }}
          </span>
        </div>
      </template>
    </header>

    <DynamicScroller
      v-if="conv"
      ref="scrollerRef"
      class="mc-msgpane__list"
      :items="displayItems"
      :min-item-size="52"
      key-field="key"
      @scroll="onScroll"
    >
      <template #before>
        <div v-if="convStore.loadingMessages" class="text-placeholder mc-msgpane__loading">加载中…</div>
        <div v-else-if="!convStore.hasMoreByConv[conv.id]" class="text-placeholder mc-msgpane__loading">没有更早的消息了</div>
      </template>

      <template #default="{ item }">
        <DynamicScrollerItem :item="item" :active="true">
          <template v-if="item.kind === 'date'">
            <div class="mc-msgpane__date text-placeholder">{{ item.label }}</div>
          </template>
          <template v-else-if="item.msg">
            <div class="mc-msg" :class="{ mine: senderOf(item.msg) }">
              <AvatarBadge
                v-if="!senderOf(item.msg)"
                :name="conv?.type === 1 ? conv.peer?.nickname ?? '?' : memberName(item.msg.senderId)"
                :color="senderAvatarColor(item.msg)"
                :size="32"
              />
              <div class="mc-msg__body">
                <div v-if="conv?.type === 2 && !senderOf(item.msg)" class="mc-msg__sender text-placeholder">
                  {{ memberName(item.msg.senderId) }}
                </div>

                <!-- 撤回 -->
                <div v-if="item.msg.isRecalled" class="mc-msg__bubble recalled">消息已撤回</div>

                <!-- 自定义类型：插件白名单渲染 -->
                <div v-else-if="item.msg.msgType !== 'text'" class="mc-msg__bubble custom">
                  <DeclarativeNodes v-if="isRenderedOk(item.msg)" :nodes="renderedNodes(item.msg)" />
                  <span v-else class="text-placeholder">[{{ item.msg.msgType }} 消息]</span>
                </div>

                <!-- 普通文本 -->
                <div v-else class="mc-msg__bubble" :class="{ pending: item.msg.pending, failed: item.msg.failed }">
                  <span class="mc-msg__text">{{ item.msg.content }}</span>
                  <span v-if="canRecall(item.msg)" class="mc-msg__recall" title="撤回" @click="recall(item.msg)">撤回</span>
                  <span v-if="item.msg.failed" class="mc-msg__resend" @click="convStore.resend(item.msg)">重发</span>
                </div>

                <div class="mc-msg__meta text-placeholder">
                  <template v-if="senderOf(item.msg) && conv?.type === 1">
                    {{ peerReadUpTo >= item.msg.id ? '已读' : '未读' }} ·
                  </template>
                  {{ fmtTime(item.msg.createdAt) }}
                </div>
              </div>
            </div>
          </template>
        </DynamicScrollerItem>
      </template>
    </DynamicScroller>

    <div v-if="typingNames.length" class="mc-msgpane__typing text-placeholder">
      {{ typingNames.join('、') }} 正在输入…
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
  height: 56px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  padding: 0 16px;
  border-bottom: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
}

.mc-msgpane__title {
  font-weight: 600;
}

.mc-msgpane__sub {
  font-size: 12px;
  font-weight: 400;
  margin-left: 8px;
}

.mc-msgpane__list {
  flex: 1;
  min-height: 0;
  padding: 12px 16px;
}

.mc-msgpane__loading {
  text-align: center;
  padding: 8px 0;
  font-size: 12px;
}

.mc-msgpane__date {
  text-align: center;
  font-size: 12px;
  padding: 10px 0 4px;
}

.mc-msg {
  display: flex;
  gap: 8px;
  margin: 6px 0;
}

.mc-msg.mine {
  flex-direction: row-reverse;
}

.mc-msg__body {
  max-width: 62%;
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

.mc-msg__bubble {
  position: relative;
  background: var(--td-bg-color-container);
  border-radius: 8px;
  padding: 8px 12px;
  word-break: break-word;
  white-space: pre-wrap;
  box-shadow: var(--td-shadow-1);
}

.mc-msg.mine .mc-msg__bubble {
  background: var(--td-brand-color);
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
  box-shadow: none;
  padding: 2px 0;
}

.mc-msg__bubble.custom {
  white-space: normal;
}

.mc-msg__text {
  user-select: text;
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
}

.mc-msg__recall {
  right: -36px;
}

.mc-msg.mine .mc-msg__recall {
  left: -36px;
  right: auto;
}

.mc-msg:hover .mc-msg__recall {
  opacity: 1;
}

.mc-msg__resend {
  right: -36px;
  color: var(--td-error-color);
  opacity: 1;
}

.mc-msg__meta {
  font-size: 11px;
  margin-top: 2px;
  user-select: none;
}

.mc-msgpane__typing {
  height: 22px;
  flex-shrink: 0;
  padding: 0 16px;
  font-size: 12px;
}
</style>
