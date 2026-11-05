<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { ConfigProvider as TConfigProvider } from 'tdesign-vue-next'
import type { GlobalConfigProvider } from 'tdesign-vue-next'
import enConfig from 'tdesign-vue-next/es/locale/en_US'
import zhConfig from 'tdesign-vue-next/es/locale/zh_CN'

import { usePrefsStore } from '@/stores/prefs'
import { useSiteStore } from '@/stores/site'

const prefs = usePrefsStore()
const site = useSiteStore()

onMounted(() => {
  // 站点名 / 注册开关 / 撤回时限等：登录页与聊天区都要用
  void site.load()
})

// TDesign 组件内部文案（分页、空态等）跟随界面语言。
// locale 包内的数组是只读字面量类型，与 GlobalConfigProvider 的可变类型不完全一致，此处按约定断言。
const globalConfig = computed(
  () => (prefs.lang === 'en-US' ? enConfig : zhConfig) as unknown as GlobalConfigProvider,
)
</script>

<template>
  <TConfigProvider :global-config="globalConfig">
    <RouterView v-slot="{ Component }">
      <Transition name="mc-route" mode="out-in">
        <component :is="Component" />
      </Transition>
    </RouterView>
  </TConfigProvider>
</template>
