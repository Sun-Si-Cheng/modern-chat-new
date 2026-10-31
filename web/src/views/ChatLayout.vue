<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Button as TButton, MessagePlugin } from 'tdesign-vue-next'
import { ChatDoubleFilledIcon } from 'tdesign-icons-vue-next'

import AddFriendDialog from '@/components/chat/AddFriendDialog.vue'
import AppRail from '@/components/chat/AppRail.vue'
import ConvSidebar from '@/components/chat/ConvSidebar.vue'
import DetailPanel from '@/components/chat/DetailPanel.vue'
import GroupPane from '@/components/chat/GroupPane.vue'
import MessagePane from '@/components/chat/MessagePane.vue'
import PluginDrawer from '@/components/chat/PluginDrawer.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { usePluginHost } from '@/composables/usePluginHost'
import { useWebSocket } from '@/composables/useWebSocket'
import { useAuthStore } from '@/stores/auth'
import { useConversationsStore } from '@/stores/conversations'
import { useFriendsStore } from '@/stores/friends'
import { usePluginsStore } from '@/stores/plugins'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
const pluginsStore = usePluginsStore()
const host = usePluginHost()
const { t, te } = useAppI18n()

/** 第二栏视图：会话列表 / 联系人（由图标栏驱动） */
const paneView = ref<'chats' | 'contacts'>('chats')
const pluginDrawerVisible = ref(false)
const addFriendVisible = ref(false)

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
    await Promise.all([convStore.loadConversations(), friendsStore.loadAll()])
    await host.start().catch((e) => console.warn('[plugin host] start failed:', e))
  } catch (e) {
    void MessagePlugin.error(te(e))
  }
  // 设置页的「插件」入口通过 ?panel=plugins 回到聊天页并直接打开面板
  if (route.query.panel === 'plugins') {
    pluginDrawerVisible.value = true
    void router.replace({ path: '/chat' })
  }
})

// 离开聊天页（登出 / 切到设置或后台 / 被踢）：停掉插件宿主并卸载 iframe 与监听器
onUnmounted(() => {
  host.stop()
  pluginsStore.reset()
})
</script>

<template>
  <div class="mc-layout" :class="{ 'has-conv': !!convStore.currentConv }">
    <AppRail v-model:view="paneView" />
    <GroupPane v-if="paneView === 'chats'" />
    <ConvSidebar :view="paneView" @open-add-friend="openAddFriend" />
    <div class="mc-layout__main">
      <Transition name="mc-swap" mode="out-in">
        <MessagePane v-if="convStore.currentConv" :key="convStore.currentConv.id" />
        <div v-else class="mc-layout__empty">
          <div class="mc-empty">
            <div class="mc-empty__mark" aria-hidden="true">
              <!-- 用 TDesign 的实心双气泡图标（与产品 logo 造型一致），替代原先的手绘线条 -->
              <ChatDoubleFilledIcon size="26px" />
            </div>
            <h1 class="mc-empty__title">{{ t('app.name') }}</h1>
            <p class="mc-empty__slogan">{{ t('app.slogan') }}</p>
            <p class="mc-empty__hint">{{ t('chat.emptyPane') }}</p>
            <!-- 主区只留一个主行动（加好友）。插件属于低频配置，入口在设置页，
                 这里用一行文字链接兜住「新用户不知道有插件」的发现路径。 -->
            <div class="mc-empty__actions">
              <TButton v-if="paneView !== 'contacts'" theme="primary" @click="openAddFriend">
                {{ t('chat.emptyAddFriend') }}
              </TButton>
              <button type="button" class="mc-empty__link" @click="pluginDrawerVisible = true">
                {{ t('chat.emptyPlugins') }}
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </div>
    <DetailPanel v-if="convStore.currentConv" />
    <PluginDrawer v-model="pluginDrawerVisible" />
    <AddFriendDialog v-model:visible="addFriendVisible" />
  </div>
</template>

<style scoped>
.mc-layout {
  height: 100%;
  display: flex;
  overflow: hidden;
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
  background: var(--td-bg-color-page);
}

/* 空会话页承担产品门面：身份、一句定位、两个可直接执行的动作 */
.mc-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  max-width: 340px;
  text-align: center;
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
  width: 48px;
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 14px;
  background: var(--td-brand-color-1, rgba(0, 82, 217, 0.1));
  color: var(--td-brand-color);
}

.mc-empty__title {
  margin: 16px 0 0;
  font-size: 20px;
  font-weight: 600;
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
  transition: text-decoration-color var(--mc-dur-fast, 120ms) ease;
}

.mc-empty__link:hover {
  text-decoration-color: currentcolor;
}

/* 平板：收起详情面板与分组栏 */
@media (max-width: 1024px) {
  .mc-layout :deep(.mc-detail) {
    display: none;
  }

  .mc-layout :deep(.group-pane) {
    display: none;
  }
}

/* 手机：第二栏与消息区互斥显示；图标栏保留 */
@media (max-width: 768px) {
  .mc-layout.has-conv :deep(.mc-sidebar) {
    display: none;
  }

  .mc-layout:not(.has-conv) :deep(.mc-msgpane),
  .mc-layout:not(.has-conv) .mc-layout__empty {
    display: none;
  }
}
</style>

