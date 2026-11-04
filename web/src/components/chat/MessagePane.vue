<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
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
  menuTarget.value = m
  menuX.value = e.clientX
  menuY.value = e.clientY
  menuVisible.value = true
}

const menuItems = computed<ContextMenuItem[]>(() => {
  const m = menuTarget.value
  if (!m) return []
  const items: ContextMenuItem[] = []
  if (m.msgType === 'text' && m.content) {
    items.push({ key: 'copy', label: t('message.copy') })
  }
  if (canRecall(m)) {
    items.push({ key: 'recall', label: t('message.recall'), danger: true, dividerBefore: items.length > 0 })
  }
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
  if (key === 'copy') void copyMessage(m)
  if (key === 'recall') recall(m)
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
              :class="{ mine: senderOf(item.msg), 'is-recalled': item.msg.isRecalled }"
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
                  />
                </div>

                <!-- 语音：自定义气泡（宽度随时长、进度条、同时只播一条） -->
                <VoiceMessage
                  v-else-if="item.msg.msgType === 'audio'"
                  :src="item.msg.content"
                  :mine="senderOf(item.msg)"
                />

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
  /* 气泡之间的呼吸感：上下各 11px，相邻两条合计 22px（原为 12px） */
  margin: 11px 0;
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
  margin-top: 4px;
  user-select: none;
  letter-spacing: 0.01em;
  opacity: 0.9;
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
