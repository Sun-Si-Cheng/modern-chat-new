<script setup lang="ts">
import { computed, ref } from 'vue'
import { MessagePlugin, Popup as TPopup } from 'tdesign-vue-next'
import { PaletteIcon } from 'tdesign-icons-vue-next'

import { useAppI18n } from '@/composables/useAppI18n'
import {
  APP_BG_PRESETS,
  MAX_APP_BG_IMAGE_LEN,
  usePrefsStore,
  type AppBgPattern,
} from '@/stores/prefs'

/**
 * 全站页面背景选择器：预设色 / 自定义色 + 纹理 + 背景图。
 * 偏好只写入 localStorage，仅影响当前浏览器；认证页在自身场景色与特效之上叠加。
 */
withDefaults(defineProps<{ compact?: boolean }>(), { compact: false })

const prefs = usePrefsStore()
const { t } = useAppI18n()
const visible = ref(false)
const imageBusy = ref(false)

const patternOptions: Array<{ value: AppBgPattern; label: string }> = [
  { value: 'none', label: 'settings.appBgPatternNone' },
  { value: 'grid', label: 'settings.appBgPatternGrid' },
  { value: 'dots', label: 'settings.appBgPatternDots' },
  { value: 'diagonal', label: 'settings.appBgPatternDiagonal' },
]

const patternLabel = computed(
  () => t(patternOptions.find((o) => o.value === prefs.appBgPattern)?.label ?? 'settings.appBgPatternNone'),
)

/** 触发按钮上的摘要：未选色时明确显示「跟随主题」 */
const summary = computed(() => {
  if (!prefs.appBgColor && prefs.appBgPattern === 'none') return t('settings.appBgFollowTheme')
  if (prefs.appBgPattern !== 'none') return patternLabel.value
  return t('settings.appBgCustom')
})

const previewStyle = computed(() => ({
  background: prefs.appBgColor || 'var(--td-bg-color-page)',
}))

function onColorInput(e: Event) {
  prefs.setAppBgColor((e.target as HTMLInputElement).value)
}

/**
 * 背景图压缩：按最大边逐级缩小、降低质量，直到 data URL 落入存储上限。
 * 输出 webp（不支持时回退 jpeg），避免原图直存撑爆 localStorage。
 */
async function toBackgroundDataURL(file: File): Promise<string> {
  const bitmap = await createImageBitmap(file)
  try {
    const canvas = document.createElement('canvas')
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('canvas unavailable')

    const attempts = [
      { max: 1920, quality: 0.82 },
      { max: 1600, quality: 0.78 },
      { max: 1280, quality: 0.72 },
      { max: 1024, quality: 0.66 },
      { max: 800, quality: 0.6 },
    ]
    for (const a of attempts) {
      const scale = Math.min(1, a.max / Math.max(bitmap.width, bitmap.height))
      canvas.width = Math.max(1, Math.round(bitmap.width * scale))
      canvas.height = Math.max(1, Math.round(bitmap.height * scale))
      ctx.clearRect(0, 0, canvas.width, canvas.height)
      ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height)

      let url = canvas.toDataURL('image/webp', a.quality)
      // 浏览器不支持 webp 时会回退成 png，改用 jpeg 控制体积
      if (!url.startsWith('data:image/webp')) url = canvas.toDataURL('image/jpeg', a.quality)
      if (url.length <= MAX_APP_BG_IMAGE_LEN) return url
    }
  } finally {
    bitmap.close?.()
  }
  throw new Error('image too large')
}

async function onImageChange(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || imageBusy.value) return
  if (!/^image\/(png|jpeg|webp)$/.test(file.type)) {
    void MessagePlugin.warning(t('settings.appBgImageInvalid'))
    return
  }

  imageBusy.value = true
  try {
    const url = await toBackgroundDataURL(file)
    if (prefs.setAppBgImage(url)) void MessagePlugin.success(t('settings.appBgImageOk'))
    else void MessagePlugin.error(t('settings.appBgImageTooLarge'))
  } catch {
    void MessagePlugin.error(t('settings.appBgImageTooLarge'))
  } finally {
    imageBusy.value = false
  }
}
</script>

<template>
  <TPopup v-model:visible="visible" trigger="click" placement="bottom-right" :show-arrow="false" attach="body">
    <button
      v-if="compact"
      type="button"
      class="abg-trigger abg-trigger--icon"
      :aria-label="t('settings.appBackground')"
      :title="t('settings.appBackground')"
    >
      <PaletteIcon />
    </button>
    <button v-else type="button" class="abg-trigger" :aria-label="t('settings.appBackground')">
      <span class="abg-trigger__swatch" :style="previewStyle" />
      <span>{{ summary }}</span>
    </button>

    <template #content>
      <div class="abg-panel">
        <div class="abg-panel__label">{{ t('settings.appBgColor') }}</div>
        <div class="abg-panel__colors">
          <button
            v-for="c in APP_BG_PRESETS"
            :key="c"
            type="button"
            class="abg-swatch"
            :class="{ 'is-active': prefs.appBgColor.toLowerCase() === c.toLowerCase() }"
            :style="{ background: c }"
            :aria-label="c"
            :aria-pressed="prefs.appBgColor.toLowerCase() === c.toLowerCase()"
            @click="prefs.setAppBgColor(c)"
          />
          <label class="abg-swatch abg-swatch--custom" :title="t('prefs.sceneCustom')">
            <input type="color" :value="prefs.appBgColor || '#6ea8ff'" @input="onColorInput" />
          </label>
        </div>

        <div class="abg-panel__label">{{ t('settings.appBgPattern') }}</div>
        <div class="abg-panel__patterns">
          <button
            v-for="opt in patternOptions"
            :key="opt.value"
            type="button"
            class="abg-pattern"
            :class="{ 'is-active': prefs.appBgPattern === opt.value }"
            :aria-pressed="prefs.appBgPattern === opt.value"
            @click="prefs.setAppBgPattern(opt.value)"
          >
            {{ t(opt.label) }}
          </button>
        </div>

        <div class="abg-panel__label">{{ t('settings.appBgImage') }}</div>
        <div class="abg-image">
          <span
            v-if="prefs.appBgImage"
            class="abg-image__thumb"
            :style="{ backgroundImage: `url(${prefs.appBgImage})` }"
          />
          <label class="abg-image__upload" :class="{ 'is-busy': imageBusy }">
            <input
              type="file"
              accept="image/png,image/jpeg,image/webp"
              :disabled="imageBusy"
              @change="onImageChange"
            />
            {{
              imageBusy
                ? t('settings.appBgImageProcessing')
                : prefs.appBgImage
                  ? t('settings.appBgImageReplace')
                  : t('settings.appBgImageUpload')
            }}
          </label>
          <button
            v-if="prefs.appBgImage"
            type="button"
            class="abg-image__remove"
            @click="prefs.setAppBgImage('')"
          >
            {{ t('settings.appBgImageRemove') }}
          </button>
        </div>
        <p class="abg-image__hint">{{ t('settings.appBgImageHint') }}</p>

        <button type="button" class="abg-panel__reset" @click="prefs.resetAppBg()">
          {{ t('prefs.sceneReset') }}
        </button>
      </div>
    </template>
  </TPopup>
</template>

<style scoped>
/* 非紧凑形态：设置页右侧的选择器，显示色块 + 当前摘要 */
.abg-trigger {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px 4px 5px;
  border: 1px solid var(--td-component-border);
  border-radius: 999px;
  background: var(--td-bg-color-container);
  font: inherit;
  font-size: 13px;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  transition:
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    color var(--mc-dur-fast) var(--mc-ease-out);
}

.abg-trigger:hover {
  border-color: var(--td-brand-color);
  color: var(--td-text-color-primary);
}

.abg-trigger__swatch {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  border-radius: 50%;
  border: 1px solid var(--mc-hairline);
}

/* 紧凑形态：聊天页标题栏的纯图标入口 */
.abg-trigger--icon {
  width: 32px;
  height: 32px;
  padding: 0;
  justify-content: center;
  border: none;
  background: transparent;
  color: var(--td-text-color-secondary);
  border-radius: var(--mc-radius-sm);
}

.abg-trigger--icon:hover {
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-primary);
}

.abg-trigger:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}

/* ===== 面板 ===== */
.abg-panel {
  width: 288px;
  padding: 14px;
}

.abg-panel__label {
  font-size: 12px;
  color: var(--mc-text-faint);
  margin-bottom: 8px;
}

.abg-panel__colors {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 14px;
}

.abg-swatch {
  width: 26px;
  height: 26px;
  padding: 0;
  border-radius: 50%;
  border: 1px solid var(--mc-hairline);
  cursor: pointer;
  transition:
    transform var(--mc-dur-fast) var(--mc-ease-out),
    box-shadow var(--mc-dur-fast) var(--mc-ease-out);
}

.abg-swatch:hover {
  transform: scale(1.08);
}

.abg-swatch.is-active {
  box-shadow:
    0 0 0 2px var(--td-bg-color-container),
    0 0 0 4px var(--td-brand-color);
}

.abg-swatch--custom {
  position: relative;
  background: conic-gradient(from 210deg, #7ec4ff, #60e2c8, #aa96ff, #ffb37e, #7ec4ff);
  overflow: hidden;
}

.abg-swatch--custom input {
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

.abg-panel__patterns {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.abg-pattern {
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

.abg-pattern:hover {
  border-color: var(--td-brand-color);
  color: var(--td-brand-color);
}

.abg-pattern.is-active {
  background: var(--td-brand-color);
  border-color: var(--td-brand-color);
  color: #fff;
}

.abg-pattern:focus-visible,
.abg-swatch:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}

/* ===== 背景图片 ===== */
.abg-image {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.abg-image__thumb {
  width: 44px;
  height: 30px;
  flex-shrink: 0;
  border-radius: var(--mc-radius-sm);
  border: 1px solid var(--mc-hairline);
  background-size: cover;
  background-position: center;
}

.abg-image__upload {
  position: relative;
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  border: 1px solid var(--td-component-border);
  border-radius: 999px;
  background: var(--td-bg-color-container);
  font-size: 12px;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  transition:
    border-color var(--mc-dur-fast) var(--mc-ease-out),
    color var(--mc-dur-fast) var(--mc-ease-out);
}

.abg-image__upload:hover {
  border-color: var(--td-brand-color);
  color: var(--td-brand-color);
}

.abg-image__upload.is-busy {
  opacity: 0.6;
  pointer-events: none;
}

.abg-image__upload input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}

.abg-image__remove {
  padding: 0;
  border: none;
  background: none;
  font: inherit;
  font-size: 12px;
  color: var(--mc-text-faint);
  cursor: pointer;
}

.abg-image__remove:hover {
  color: var(--td-error-color);
}

.abg-image__hint {
  margin: 0 0 8px;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--mc-text-faint);
}

.abg-panel__reset {
  margin-top: 4px;
  padding: 0;
  border: none;
  background: none;
  font: inherit;
  font-size: 12px;
  color: var(--mc-text-faint);
  cursor: pointer;
}

.abg-panel__reset:hover {
  color: var(--mc-link);
  text-decoration: underline;
}
</style>
