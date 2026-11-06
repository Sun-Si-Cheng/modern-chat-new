<script setup lang="ts">
import UiButton from '@/components/ui/UiButton.vue'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { UsergroupIcon } from 'tdesign-icons-vue-next'

import AddFriendDialog from '@/components/chat/AddFriendDialog.vue'
import AnnouncementDialog from '@/components/chat/AnnouncementDialog.vue'
import AppRail from '@/components/chat/AppRail.vue'
import ConvSidebar from '@/components/chat/ConvSidebar.vue'
import DetailPanel from '@/components/chat/DetailPanel.vue'
import FavoriteDrawer from '@/components/chat/FavoriteDrawer.vue'
import GroupPane from '@/components/chat/GroupPane.vue'
import MessagePane from '@/components/chat/MessagePane.vue'
import NotificationDrawer from '@/components/chat/NotificationDrawer.vue'
import PluginDrawer from '@/components/chat/PluginDrawer.vue'
import PaneResizer from '@/components/common/PaneResizer.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { lastSeenAnnouncementId, markAnnouncementSeen } from '@/composables/announcement'
import { showMessageNotification } from '@/composables/notifications'
import { usePluginHost } from '@/composables/usePluginHost'
import { useWebSocket } from '@/composables/useWebSocket'
import { convApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFavoritesStore } from '@/stores/favorites'
import { useFriendsStore } from '@/stores/friends'
import { PANE_LIMITS, useLayoutStore, type PaneKey } from '@/stores/layout'
import { usePluginsStore } from '@/stores/plugins'
import { useWsStore } from '@/stores/ws'
import type { ServerFrame } from '@/types/proto'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const layout = useLayoutStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const favoritesStore = useFavoritesStore()
const pluginsStore = usePluginsStore()
const host = usePluginHost()
const ws = useWsStore()
const { t, te } = useAppI18n()

/** 第二栏视图：会话列表 / 联系人（由图标栏驱动） */
const paneView = ref<'chats' | 'contacts'>('chats')
const pluginDrawerVisible = ref(false)
const addFriendVisible = ref(false)
const favoriteDrawerVisible = ref(false)
const notificationDrawerVisible = ref(false)

// ===== 全站公告弹窗 =====

const announcementVisible = ref(false)
const announcementContent = ref('')
let offAnnouncement: (() => void) | null = null
let offNotify: (() => void) | null = null

const systemConversation = computed(
  () => convStore.conversations.find((c) => c.type === 3) ?? null,
)

/** 有新公告时弹窗；已读位置存本地，同一条公告只弹一次 */
function showAnnouncement(id: number, content: string) {
  if (!content) return
  markAnnouncementSeen(id)
  announcementContent.value = content
  announcementVisible.value = true
  // 系统通知会话不在列表展示，但未读仍需清掉，避免后端留下永久的未读数
  const convId = systemConversation.value?.id
  if (convId) {
    const conv = convStore.conversations.find((c) => c.id === convId)
    if (conv) conv.unreadCount = 0
    void convApi.read(convId, id).catch(() => undefined)
  }
}

/** 在线收到公告帧：系统会话已加载时直接弹 */
function onAnnouncementFrame(frame: ServerFrame) {
  if (frame.type !== 'message' || !systemConversation.value) return
  if (frame.convId !== systemConversation.value.id) return
  // 自己发出的公告不弹（站长发完公告回到聊天页时不该看到自己的公告）
  if (frame.from.id === auth.user?.id) {
    markAnnouncementSeen(frame.id)
    return
  }
  showAnnouncement(frame.id, frame.content)
}

/** 新消息系统通知：页面不可见、非自己发送、会话未免打扰时弹出 */
function notifyPreview(frame: Extract<ServerFrame, { type: 'message' }>): string {
  if (frame.msgType === 'image') return t('chat.previewImage')
  if (frame.msgType === 'audio') return t('chat.previewVoice')
  if (frame.msgType === 'sticker') return t('message.quoteSticker')
  if (frame.msgType === 'video') return t('chat.previewVideo')
  if (frame.msgType !== 'text') return t('message.unknownType', { type: frame.msgType })
  return frame.content
}

function onMessageNotify(frame: ServerFrame) {
  if (frame.type !== 'message') return
  if (frame.from.id === auth.user?.id) return
  const conv = convStore.conversations.find((c) => c.id === frame.convId)
  if (!conv || conv.muted) return
  const name = conv.type === 1 ? (conv.peer?.nickname ?? t('app.name')) : (conv.name ?? t('app.name'))
  showMessageNotification(name, notifyPreview(frame), () => {
    void convStore.openConversation(frame.convId)
  })
}

// 离线期间产生的公告没有 WS 帧：以系统会话最后一条消息为准补弹
watch(
  () => systemConversation.value?.lastMessageId ?? null,
  (id) => {
    if (id === null || id <= lastSeenAnnouncementId()) return
    showAnnouncement(id, systemConversation.value?.lastMessagePreview ?? '')
  },
  { immediate: true },
)

/** 详情面板是否显示（依赖当前会话） */
const showDetail = computed(() => !!convStore.currentConv)
/** 是否有抽屉处于展开状态（决定遮罩显示与 Esc 行为） */
const hasOpenDrawer = computed(() => layout.opened.group || layout.opened.conv || layout.opened.detail)
/**
 * 断线提示：只在此前成功连过之后才显示，避免首屏「连接中」闪一条黄条。
 * 断线期间消息发不出去（会自动进失败态并可重发），先让用户知道是网络问题。
 */
const offline = computed(() => ws.hasConnected && !ws.connected)

/** Esc 收起当前展开的抽屉 */
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && hasOpenDrawer.value) layout.closeAll()
}

/**
 * 双击分隔条：在「常驻 / 抽屉」之间切换。
 * 切成抽屉时立即展开 —— 否则该区域会凭空消失，用户会以为功能丢了。
 */
function togglePane(key: PaneKey) {
  layout.toggleDrawer(key)
  if (layout.panes[key].drawer) layout.open(key)
}

function openAddFriend() {
  addFriendVisible.value = true
}

// WS 连接生命周期（挂载连接、卸载断开、上行出口绑定）
useWebSocket()

onMounted(async () => {
  if (!auth.token) {
    await router.push('/login')
    return
  }
  await auth.fetchMe()
  if (!auth.user) {
    await router.push('/login')
    return
  }
  try {
    await Promise.all([
      convStore.loadConversations(),
      friendsStore.loadAll(),
      // 收藏 id 供消息菜单判断「收藏 / 取消收藏」，拉取失败不阻断进入聊天
      favoritesStore.load().catch(() => undefined),
    ])
    await host.start().catch((e) => console.warn('[plugin host] start failed:', e))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
  // 设置页的「插件」入口通过 ?panel=plugins 回到聊天页并直接打开面板
  if (route.query.panel === 'plugins') {
    pluginDrawerVisible.value = true
    void router.replace({ path: '/chat' })
  }
  offAnnouncement = ws.onFrame(onAnnouncementFrame)
  offNotify = ws.onFrame(onMessageNotify)
  window.addEventListener('keydown', onKeydown)
})

// 离开聊天页（登出 / 切到设置或后台 / 被踢）：停掉插件宿主并卸载 iframe 与监听器
onUnmounted(() => {
  offAnnouncement?.()
  offAnnouncement = null
  offNotify?.()
  offNotify = null
  host.stop()
  pluginsStore.reset()
  favoritesStore.reset()
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div class="mc-layout" :style="layout.vars" :class="{ 'has-conv': !!convStore.currentConv }">
    <!-- 断线提示：重连成功即消失；期间消息会进失败态，可直接点「重发」 -->
    <div v-if="offline" class="mc-layout__offline" role="status">
      {{ t('chat.reconnecting') }}
    </div>

    <AppRail
      v-model:view="paneView"
      @open-favorites="favoriteDrawerVisible = true"
      @open-notifications="notificationDrawerVisible = true"
    />

    <!-- 分组栏：可拖拽调宽；双击分隔条切换「常驻 / 抽屉」 -->
    <template v-if="paneView === 'chats'">
      <GroupPane
        v-if="!layout.hidden('group')"
        class="mc-pane mc-pane--left"
        :class="{ 'is-drawer': layout.panes.group.drawer }"
      />
      <PaneResizer
        v-if="!layout.panes.group.drawer"
        :model-value="layout.panes.group.width"
        :min="PANE_LIMITS.group.min"
        :max="PANE_LIMITS.group.max"
        :label="t('layout.resizeGroup')"
        @update:model-value="layout.setWidth('group', $event)"
        @dblclick="togglePane('group')"
      />
    </template>

    <!-- 会话列表 -->
    <ConvSidebar
      v-if="!layout.hidden('conv')"
      class="mc-pane mc-pane--left"
      :class="{ 'is-drawer': layout.panes.conv.drawer }"
      :view="paneView"
      @open-add-friend="openAddFriend"
    />
    <PaneResizer
      v-if="!layout.panes.conv.drawer"
      :model-value="layout.panes.conv.width"
      :min="PANE_LIMITS.conv.min"
      :max="PANE_LIMITS.conv.max"
      :label="t('layout.resizeConv')"
      @update:model-value="layout.setWidth('conv', $event)"
      @dblclick="togglePane('conv')"
    />
    <div class="mc-layout__main">
      <Transition name="mc-swap" mode="out-in">
        <MessagePane v-if="convStore.currentConv" :key="convStore.currentConv.id" />
        <div v-else class="mc-layout__empty">
          <div class="mc-empty">
            <div class="mc-empty__mark" aria-hidden="true">
              <!-- 产品 favicon 作为空态门面标记 -->
              <img src="/favicon.ico" alt="" class="mc-empty__mark-img" />
            </div>
            <h1 class="mc-empty__title">{{ t('app.name') }}</h1>
            <p class="mc-empty__slogan">{{ t('app.slogan') }}</p>
            <p class="mc-empty__hint">{{ t('chat.emptyPane') }}</p>
            <!-- 主区只留一个主行动（加好友）。插件属于低频配置，入口在设置页，
                 这里用一行文字链接兜住「新用户不知道有插件」的发现路径。 -->
            <div class="mc-empty__actions">
              <UiButton v-if="paneView !== 'contacts'" theme="primary" @click="openAddFriend">
                {{ t('chat.emptyAddFriend') }}
              </UiButton>
              <button type="button" class="mc-empty__link" @click="pluginDrawerVisible = true">
                {{ t('chat.emptyPlugins') }}
              </button>
            </div>
          </div>
        </div>
      </Transition>
      <!-- 详情抽屉形态下的入口按钮（浮在主区右上角） -->
      <UiButton
        v-if="showDetail && layout.panes.detail.drawer"
        class="mc-layout__detail-toggle"
        variant="text"
        size="small"
        :aria-label="t('layout.openDetail')"
        :title="t('layout.openDetail')"
        @click="layout.open('detail')"
      >
        <UsergroupIcon size="16px" />
      </UiButton>
    </div>

    <!-- 详情面板：可拖拽调宽；双击分隔条切换「常驻 / 抽屉」 -->
    <PaneResizer
      v-if="showDetail && !layout.panes.detail.drawer"
      class="mc-resizer--detail"
      :model-value="layout.panes.detail.width"
      :min="PANE_LIMITS.detail.min"
      :max="PANE_LIMITS.detail.max"
      invert
      :label="t('layout.resizeDetail')"
      @update:model-value="layout.setWidth('detail', $event)"
      @dblclick="togglePane('detail')"
    />
    <!-- 布局类必须挂在 ChatLayout 自己的元素上：
         DetailPanel 是多根模板，父级 class 会整个丢失（抽屉定位/层级失效，
         遮罩盖住整个面板，表现为「能看到详情但点不了拉黑」） -->
    <div
      v-if="showDetail && !layout.hidden('detail')"
      class="mc-pane mc-pane--right"
      :class="{ 'is-drawer': layout.panes.detail.drawer }"
    >
      <DetailPanel />
    </div>
    <PluginDrawer v-model="pluginDrawerVisible" />
    <FavoriteDrawer v-model="favoriteDrawerVisible" />
    <NotificationDrawer v-model="notificationDrawerVisible" />
    <AddFriendDialog v-model:visible="addFriendVisible" />
    <AnnouncementDialog v-model:visible="announcementVisible" :content="announcementContent" />

    <!-- 抽屉遮罩：点击空白处收起 -->
    <div v-if="hasOpenDrawer" class="mc-layout__scrim" @click="layout.closeAll()" />
  </div>
</template>

<style scoped>
.mc-layout {
  height: 100%;
  display: flex;
  overflow: hidden;
  /* 抽屉浮层的定位上下文 */
  position: relative;
}

/* ===== 布局：可拖拽宽度 + 抽屉形态 ===== */

/* 抽屉形态：脱离 flex 流，浮在主区上方（不挤压消息区） */
.mc-pane.is-drawer {
  position: absolute;
  top: 0;
  bottom: 0;
  z-index: 30;
  box-shadow: 0 16px 48px -16px rgba(0, 0, 0, 0.38);
  animation: mc-drawer-in 160ms var(--mc-ease-out);
}

.mc-pane--left.is-drawer {
  left: 56px; /* 贴着图标栏右侧 */
}

.mc-pane--right.is-drawer {
  right: 0;
  animation-name: mc-drawer-in-right;
}

/* 详情面板容器：常驻时是普通 flex 子项，宽度由内部 .mc-detail 的 width 决定 */
.mc-pane--right {
  display: flex;
  flex-shrink: 0;
  min-height: 0;
}

/* 断线提示：浮在顶部居中，重连后自动消失 */
.mc-layout__offline {
  position: absolute;
  top: 10px;
  left: 50%;
  z-index: 60;
  transform: translateX(-50%);
  padding: 5px 14px;
  border-radius: 999px;
  background: var(--td-warning-color, #ed9e20);
  color: #fff;
  font-size: 12px;
  line-height: 1.5;
  white-space: nowrap;
  box-shadow: var(--mc-elev-2);
  pointer-events: none;
}

/* 抽屉遮罩：点击空白处收起 */
.mc-layout__scrim {
  position: absolute;
  inset: 0;
  z-index: 25;
  background: rgba(0, 0, 0, 0.16);
}

/* 详情抽屉的入口（浮在主区右上角） */
.mc-layout__detail-toggle {
  position: absolute;
  top: 10px;
  right: 14px;
  z-index: 12;
}

@keyframes mc-drawer-in {
  from {
    opacity: 0;
    transform: translateX(-12px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

@keyframes mc-drawer-in-right {
  from {
    opacity: 0;
    transform: translateX(12px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .mc-pane.is-drawer {
    animation: none;
  }
}

/* 会话区固定占位：切换会话/空态时只做淡入淡出，不推动右侧详情栏 */
.mc-layout__main {
  flex: 1;
  min-width: 0;
  display: flex;
}

.mc-layout__empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background-color: transparent;
}

/* 空会话页承担产品门面：身份、一句定位、两个可直接执行的动作。
 * 收进一块轻卡片，避免在自定义背景图/纹理上直接读文字。 */
.mc-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  width: 360px;
  max-width: 100%;
  padding: 36px 32px 32px;
  text-align: center;
  background: var(--td-bg-color-container);
  border: 1px solid var(--mc-hairline);
  border-radius: var(--mc-radius-lg);
  box-shadow: var(--mc-shadow-1);
}

/* 空状态逐层入场：标记 → 标题 → 定位语 → 提示 → 动作 */
.mc-empty > * {
  animation: mc-fade-up var(--mc-dur-slow) var(--mc-ease-out) both;
}

.mc-empty__mark {
  animation-name: mc-pop-in;
}

.mc-empty__title {
  animation-delay: 40ms;
}

.mc-empty__slogan {
  animation-delay: 70ms;
}

.mc-empty__hint {
  animation-delay: 100ms;
}

.mc-empty__actions {
  animation-delay: 140ms;
}

.mc-empty__mark {
  width: 52px;
  height: 52px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--mc-radius-lg);
  background: var(--td-brand-color-1, rgba(0, 82, 217, 0.1));
  color: var(--td-brand-color);
}

.mc-empty__mark-img {
  width: 32px;
  height: 32px;
  object-fit: contain;
}

.mc-empty__title {
  margin: 16px 0 0;
  font-size: var(--mc-fs-display);
  font-weight: 600;
  letter-spacing: -0.01em;
  line-height: 1.3;
}

.mc-empty__slogan {
  margin: 6px 0 0;
  font-size: 13px;
  line-height: 1.5;
  color: var(--td-text-color-secondary);
}

.mc-empty__hint {
  margin: 14px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--mc-text-faint);
}

.mc-empty__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 14px;
  margin-top: 20px;
}

/* 次要入口：文字链接，不与主按钮争夺注意力 */
.mc-empty__link {
  border: none;
  background: none;
  padding: 0;
  font: inherit;
  font-size: 13px;
  color: var(--mc-link, var(--td-brand-color));
  cursor: pointer;
  text-decoration: underline;
  text-underline-offset: 3px;
  text-decoration-color: transparent;
  transition: text-decoration-color var(--mc-dur-fast, 120ms) var(--mc-ease-in-out);
}

.mc-empty__link:hover {
  text-decoration-color: currentcolor;
}

/* 平板：收起详情面板与分组栏（抽屉形态被用户展开时仍然显示） */
@media (max-width: 1024px) {
  .mc-layout > .mc-pane--right:not(.is-drawer) {
    display: none;
  }

  .mc-layout :deep(.group-pane:not(.is-drawer)) {
    display: none;
  }

  /* 面板已经隐藏了，分隔条也要一起消失，否则屏幕上留一根拖不动的空白条 */
  .mc-layout > .mc-resizer--detail {
    display: none;
  }
}

/* 手机：第二栏与消息区互斥显示；图标栏保留 */
@media (max-width: 768px) {
  /* 详情抽屉按视口收窄：280px 在 360px 屏上会顶满，留一点主区可见（点空白可关闭） */
  .mc-layout > .mc-pane--right.is-drawer {
    width: min(88vw, 320px);
  }

  .mc-layout > .mc-pane--right.is-drawer :deep(.mc-detail) {
    width: 100%;
  }

  /* 抽屉形态不作互斥：用户主动展开时要能看到（否则点了入口像没反应） */
  .mc-layout.has-conv :deep(.mc-sidebar:not(.is-drawer)) {
    display: none;
  }

  .mc-layout:not(.has-conv) :deep(.mc-msgpane),
  .mc-layout:not(.has-conv) .mc-layout__empty {
    display: none;
  }
}
</style>

