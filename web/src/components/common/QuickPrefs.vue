<script setup lang="ts">
import UiPopup from '@/components/ui/UiPopup.vue'
import UiButton from '@/components/ui/UiButton.vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Tooltip as TTooltip } from 'tdesign-vue-next'
import { DesktopIcon, MoonIcon, PaletteIcon, SunnyIcon, TranslateIcon } from 'tdesign-icons-vue-next'

import { SCENE_PRESETS, usePrefsStore, type SceneEffect } from '@/stores/prefs'

/**
 * 认证页（登录 / 注册 / 找回 / 协议）的快捷外观设置：
 * 语言、明暗、登录页背景（颜色 + 特效）。
 * 账号与其他设置仍在登录后的 /settings。
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

const effectOptions: Array<{ value: SceneEffect; label: string }> = [
  { value: 'aurora', label: 'prefs.effectAurora' },
  { value: 'waves', label: 'prefs.effectWaves' },
  { value: 'grid', label: 'prefs.effectGrid' },
  { value: 'none', label: 'prefs.effectNone' },
]

function onColorInput(e: Event) {
  prefs.setSceneColor((e.target as HTMLInputElement).value)
}
</script>

<template>
  <div class="quick-prefs">
    <TTooltip :content="`${t('prefs.language')} · ${prefs.lang === 'zh-CN' ? '中文' : 'English'}`">
      <UiButton
        variant="text"
        shape="square"
        :aria-label="t('prefs.language')"
        @click="prefs.toggleLang()"
      >
        <template #icon><TranslateIcon /></template>
      </UiButton>
    </TTooltip>

    <TTooltip :content="`${t('prefs.theme')}：${themeLabel}`">
      <UiButton
        variant="text"
        shape="square"
        :aria-label="`${t('prefs.theme')}：${themeLabel}`"
        @click="prefs.cycleTheme()"
      >
        <template #icon><component :is="themeIcon" /></template>
      </UiButton>
    </TTooltip>

    <!-- 登录页背景：颜色 + 特效 -->
    <UiPopup placement="bottom-right">
      <TTooltip :content="t('prefs.scene')">
        <UiButton variant="text" shape="square" :aria-label="t('prefs.scene')">
          <template #icon><PaletteIcon /></template>
        </UiButton>
      </TTooltip>

      <template #content>
        <div class="scene-panel">
          <div class="scene-panel__label">{{ t('prefs.sceneColor') }}</div>
          <div class="scene-panel__colors">
            <button
              v-for="c in SCENE_PRESETS"
              :key="c"
              type="button"
              class="scene-swatch"
              :class="{ 'is-active': prefs.sceneColor.toLowerCase() === c.toLowerCase() }"
              :style="{ background: c }"
              :aria-label="c"
              :aria-pressed="prefs.sceneColor.toLowerCase() === c.toLowerCase()"
              @click="prefs.setSceneColor(c)"
            />
            <label class="scene-swatch scene-swatch--custom" :title="t('prefs.sceneCustom')">
              <input type="color" :value="prefs.sceneColor" @input="onColorInput" />
            </label>
          </div>

          <div class="scene-panel__label">{{ t('prefs.sceneEffect') }}</div>
          <div class="scene-panel__effects">
            <button
              v-for="opt in effectOptions"
              :key="opt.value"
              type="button"
              class="scene-effect"
              :class="{ 'is-active': prefs.sceneEffect === opt.value }"
              :aria-pressed="prefs.sceneEffect === opt.value"
              @click="prefs.setSceneEffect(opt.value)"
            >
              {{ t(opt.label) }}
            </button>
          </div>

          <button type="button" class="scene-panel__reset" @click="prefs.resetScene()">
            {{ t('prefs.sceneReset') }}
          </button>
        </div>
      </template>
    </UiPopup>
  </div>
</template>

<style scoped>
.quick-prefs {
  display: flex;
  align-items: center;
  gap: 2px;
  color: var(--td-text-color-secondary);
}

/* ===== 背景设置面板 ===== */
.scene-panel {
  width: 254px;
  padding: 12px;
}

.scene-panel__label {
  font-size: 12px;
  color: var(--mc-text-faint);
  margin-bottom: 8px;
}

.scene-panel__colors {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 14px;
}

.scene-swatch {
  width: 26px;
  height: 26px;
  padding: 0;
  border-radius: var(--mc-radius-sm);
  border: 1px solid rgba(0, 0, 0, 0.18);
  cursor: pointer;
  transition:
    box-shadow var(--mc-dur-fast) var(--mc-ease-out),
    transform var(--mc-dur-fast) var(--mc-ease-out);
}

.scene-swatch:hover {
  transform: scale(1.06);
}

.scene-swatch.is-active {
  box-shadow:
    0 0 0 2px var(--td-bg-color-container),
    0 0 0 4px var(--td-brand-color);
}

.scene-swatch--custom {
  position: relative;
  background: conic-gradient(from 210deg, #7ec4ff, #60e2c8, #aa96ff, #ffb37e, #7ec4ff);
  overflow: hidden;
}

.scene-swatch--custom input {
  position: absolute;
  inset: -4px;
  width: calc(100% + 8px);
  height: calc(100% + 8px);
  padding: 0;
  border: none;
  background: transparent;
  cursor: pointer;
  opacity: 0;
}

.scene-panel__effects {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.scene-effect {
  padding: 4px 10px;
  font-size: 12px;
  font-family: inherit;
  border-radius: 999px;
  border: 1px solid var(--td-component-border);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-secondary);
  cursor: pointer;
  transition:
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    background-color var(--mc-dur-fast) var(--mc-ease-out),
    color var(--mc-dur-fast) var(--mc-ease-out);
}

.scene-effect:hover {
  border-color: var(--td-brand-color);
  color: var(--td-brand-color);
}

.scene-effect.is-active {
  background: var(--td-brand-color);
  border-color: var(--td-brand-color);
  color: #fff;
}

.scene-effect:focus-visible,
.scene-swatch:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}

.scene-panel__reset {
  margin-top: 12px;
  padding: 0;
  border: none;
  background: none;
  font-size: 12px;
  font-family: inherit;
  color: var(--mc-text-faint);
  cursor: pointer;
}

.scene-panel__reset:hover {
  color: var(--mc-link);
  text-decoration: underline;
}
</style>
