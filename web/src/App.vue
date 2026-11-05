<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { ConfigProvider as TConfigProvider } from 'tdesign-vue-next'
import type { GlobalConfigProvider } from 'tdesign-vue-next'
import enConfig from 'tdesign-vue-next/es/locale/en_US'
import zhConfig from 'tdesign-vue-next/es/locale/zh_CN'
import { useRoute } from 'vue-router'

import MaintenanceView from '@/views/MaintenanceView.vue'
import { useAuthStore } from '@/stores/auth'
import { usePrefsStore } from '@/stores/prefs'
import { useSiteStore } from '@/stores/site'

const prefs = usePrefsStore()
const site = useSiteStore()
const auth = useAuthStore()
const route = useRoute()

let pollTimer: ReturnType<typeof setInterval> | undefined

onMounted(() => {
  // 站点名 / 注册开关 / 撤回时限等：登录页与聊天区都要用
  void site.load()
  // 维护模式可能在用户在线时被站长打开，每 30 秒探一次，
  // 免得非要用户刷新页面才生效（后端拦截同样实时生效，这里只是让界面跟上）
  pollTimer = setInterval(() => void site.refresh(), 30_000)
})

onBeforeUnmount(() => clearInterval(pollTimer))

// 管理员放行（否则站长把自己也锁在门外），/login 放行（管理员要能登录）
const inMaintenance = computed(
  () =>
    site.loaded &&
    site.config.maintenanceMode &&
    auth.user?.role !== 'admin' &&
    route.path !== '/login',
)

// TDesign 组件内部文案（分页、空态等）跟随界面语言。
// locale 包内的数组是只读字面量类型，与 GlobalConfigProvider 的可变类型不完全一致，此处按约定断言。
const globalConfig = computed(
  () => (prefs.lang === 'en-US' ? enConfig : zhConfig) as unknown as GlobalConfigProvider,
)
</script>

<template>
  <TConfigProvider :global-config="globalConfig">
    <MaintenanceView v-if="inMaintenance" />
    <RouterView v-else v-slot="{ Component }">
      <Transition name="mc-route" mode="out-in">
        <component :is="Component" />
      </Transition>
    </RouterView>
  </TConfigProvider>
</template>
