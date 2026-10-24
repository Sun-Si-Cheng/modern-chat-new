<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button as TButton, Tooltip as TTooltip } from 'tdesign-vue-next'
import { DesktopIcon, MoonIcon, SunnyIcon, TranslateIcon } from 'tdesign-icons-vue-next'

import { usePrefsStore } from '@/stores/prefs'

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
  <div class="mc-prefs">
    <TTooltip :content="`${t('prefs.language')} · ${prefs.lang === 'zh-CN' ? '中文' : 'English'}`">
      <TButton
        class="mc-prefs__btn"
        variant="text"
        shape="square"
        :aria-label="t('prefs.language')"
        @click="prefs.toggleLang()"
      >
        <template #icon><TranslateIcon /></template>
        <span class="mc-prefs__label">{{ prefs.lang === 'zh-CN' ? '中' : 'EN' }}</span>
      </TButton>
    </TTooltip>

    <TTooltip :content="`${t('prefs.theme')}：${themeLabel}`">
      <TButton
        class="mc-prefs__btn"
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
.mc-prefs {
  display: flex;
  align-items: center;
  gap: 2px;
}

.mc-prefs__btn {
  color: var(--td-text-color-secondary);
}

.mc-prefs__label {
  font-size: 12px;
  line-height: 1;
  margin-left: 2px;
}
</style>
