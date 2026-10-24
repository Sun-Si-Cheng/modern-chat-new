<script setup lang="ts">
import { computed } from 'vue'
import { ConfigProvider as TConfigProvider } from 'tdesign-vue-next'
import type { GlobalConfigProvider } from 'tdesign-vue-next'
import enConfig from 'tdesign-vue-next/es/locale/en_US'
import zhConfig from 'tdesign-vue-next/es/locale/zh_CN'

import { usePrefsStore } from '@/stores/prefs'

const prefs = usePrefsStore()

// TDesign 组件内部文案（分页、空态等）跟随界面语言。
// locale 包内的数组是只读字面量类型，与 GlobalConfigProvider 的可变类型不完全一致，此处按约定断言。
const globalConfig = computed(
  () => (prefs.lang === 'en-US' ? enConfig : zhConfig) as unknown as GlobalConfigProvider,
)
</script>

<template>
  <TConfigProvider :global-config="globalConfig">
    <RouterView />
  </TConfigProvider>
</template>
