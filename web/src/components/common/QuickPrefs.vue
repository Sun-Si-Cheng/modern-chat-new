<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button as TButton, Tooltip as TTooltip } from 'tdesign-vue-next'
import { DesktopIcon, MoonIcon, SunnyIcon, TranslateIcon } from 'tdesign-icons-vue-next'

import { usePrefsStore } from '@/stores/prefs'

/**
 * 认证页（登录 / 注册 / 找回 / 协议）的轻量切换：只有语言与明暗。
 * 完整外观设置（背景色、特效等）在登录后的 /settings 里。
 */
const prefs = usePrefsStore()
const { t } = useI18n()

const themeIcon = computed(() => {
  if (prefs.theme === 'dark') return MoonIcon
  if (prefs.theme === 'light') return SunnyIcon
  return DesktopIcon
})

const themeLabel = computed(() => {
  if (prefs.theme === 'dark') return t('prefs.themeDark')
  if (prefs.theme === 'light') return t('prefs.themeLight')
  return t('prefs.themeAuto')
})
</script>

<template>
  <div class="quick-prefs">
    <TTooltip :content="`${t('prefs.language')} · ${prefs.lang === 'zh-CN' ? '中文' : 'English'}`">
      <TButton
        variant="text"
        shape="square"
        :aria-label="t('prefs.language')"
        @click="prefs.toggleLang()"
      >
        <template #icon><TranslateIcon /></template>
      </TButton>
    </TTooltip>

    <TTooltip :content="`${t('prefs.theme')}：${themeLabel}`">
      <TButton
        variant="text"
        shape="square"
        :aria-label="`${t('prefs.theme')}：${themeLabel}`"
        @click="prefs.cycleTheme()"
      >
        <template #icon><component :is="themeIcon" /></template>
      </TButton>
    </TTooltip>
  </div>
</template>

<style scoped>
.quick-prefs {
  display: flex;
  align-items: center;
  gap: 2px;
  color: var(--td-text-color-secondary);
}
</style>
