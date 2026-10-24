<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Button as TButton, MessagePlugin } from 'tdesign-vue-next'

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

const router = useRouter()
const auth = useAuthStore()
const convStore = useConversationsStore()
const friendsStore = useFriendsStore()
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
})
</script>

<template>
  <div class="mc-layout" :class="{ 'has-conv': !!convStore.currentConv }">
    <AppRail
      v-model:view="paneView"
      @open-plugins="pluginDrawerVisible = true"
      @open-add-friend="openAddFriend"
    />
    <GroupPane v-if="paneView === 'chats'" />
    <ConvSidebar :view="paneView" @open-add-friend="openAddFriend" />
    <MessagePane v-if="convStore.currentConv" :key="convStore.currentConv.id" />
    <div v-else class="mc-layout__empty">
      <div class="mc-empty">
        <div class="mc-empty__mark" aria-hidden="true">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
            <path
              d="M5 5.8C5 4.8 5.8 4 6.8 4h10.4c1 0 1.8.8 1.8 1.8v8.4c0 1-.8 1.8-1.8 1.8H10l-4.2 3.2c-.5.4-1.3 0-1.3-.7V14.2c-.3-.2-.5-.6-.5-1.1V5.8Z"
              stroke="currentColor"
              stroke-width="1.6"
              stroke-linejoin="round"
            />
            <path d="M8.5 8.5h7M8.5 11.5h4.5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
        </div>
        <h1 class="mc-empty__title">{{ t('app.name') }}</h1>
        <p class="mc-empty__slogan">{{ t('app.slogan') }}</p>
        <p class="mc-empty__hint">{{ t('chat.emptyPane') }}</p>
        <div class="mc-empty__actions">
          <TButton theme="primary" @click="openAddFriend">{{ t('chat.emptyAddFriend') }}</TButton>
          <TButton variant="outline" @click="pluginDrawerVisible = true">{{ t('chat.emptyPlugins') }}</TButton>
        </div>
      </div>
    </div>
    <DetailPanel v-if="convStore.currentConv" />
    <PluginDrawer v-model:visible="pluginDrawerVisible" />
    <AddFriendDialog v-model:visible="addFriendVisible" />
  </div>
</template>

<style scoped>
.mc-layout {
  height: 100%;
  display: flex;
  overflow: hidden;
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
  justify-content: center;
  gap: 8px;
  margin-top: 20px;
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

