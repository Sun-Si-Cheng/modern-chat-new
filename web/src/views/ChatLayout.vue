<script setup lang="ts">
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'

import ConvSidebar from '@/components/chat/ConvSidebar.vue'
import DetailPanel from '@/components/chat/DetailPanel.vue'
import MessagePane from '@/components/chat/MessagePane.vue'
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
    <ConvSidebar />
    <MessagePane v-if="convStore.currentConv" :key="convStore.currentConv.id" />
    <div v-else class="mc-layout__empty">
      <div class="text-placeholder">{{ t('chat.emptyPane') }}</div>
    </div>
    <DetailPanel v-if="convStore.currentConv" />
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
  background: var(--td-bg-color-page);
}

/* 平板：收起详情面板 */
@media (max-width: 1024px) {
  .mc-layout :deep(.mc-detail) {
    display: none;
  }
}

/* 手机：侧栏与消息区互斥显示，由返回键切换 */
@media (max-width: 768px) {
  .mc-layout.has-conv .mc-sidebar {
    display: none;
  }

  .mc-layout:not(.has-conv) :deep(.mc-msgpane),
  .mc-layout:not(.has-conv) .mc-layout__empty {
    display: none;
  }
}
</style>
