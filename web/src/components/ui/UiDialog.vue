<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import UiButton from './UiButton.vue'
import { useAppI18n } from '@/composables/useAppI18n'
import { handleTabKey, lockBodyScroll, unlockBodyScroll } from '@/composables/useOverlay'

/**
 * 模态对话框（UnoCSS 实现，替代 TDesign Dialog）。
 *
 * - `v-model:visible`、`header`、`width`、`footer`
 * - `confirm-btn` 可自定义确认按钮文案与主题（兼容 TDesign 的写法）
 * - 关闭途径：右上角 ×、点遮罩、Esc；关闭后把焦点还给打开前的元素
 */
const props = withDefaults(
  defineProps<{
    visible?: boolean
    header?: string
    width?: string | number
    /** false 时不渲染底部按钮区（纯展示型弹窗） */
    footer?: boolean
    confirmBtn?: { content?: string; theme?: 'primary' | 'danger' | 'default' }
    confirmBtnLoading?: boolean
    cancelBtn?: string
  }>(),
  { footer: true, width: 480, confirmBtnLoading: false },
)

const emit = defineEmits<{
  'update:visible': [value: boolean]
  confirm: []
  close: []
}>()

const { t } = useAppI18n()
const panelEl = ref<HTMLElement | null>(null)
let restoreFocus: HTMLElement | null = null

const widthStyle = computed(() =>
  typeof props.width === 'number' ? `${props.width}px` : props.width,
)

const confirmTheme = computed<'primary' | 'danger' | 'default'>(() => props.confirmBtn?.theme ?? 'primary')

function close() {
  emit('update:visible', false)
  emit('close')
}

function onConfirm() {
  if (props.confirmBtnLoading) return
  emit('confirm')
}

let scrollLocked = false

function onKeydown(e: KeyboardEvent) {
  if (!props.visible) return
  if (e.key === 'Escape') {
    close()
    return
  }
  // Tab 环在弹窗内：否则键盘用户会 Tab 到背景页的按钮上
  handleTabKey(e, panelEl.value)
}

watch(
  () => props.visible,
  async (v) => {
    if (v) {
      restoreFocus = document.activeElement as HTMLElement | null
      if (!scrollLocked) {
        lockBodyScroll()
        scrollLocked = true
      }
      await nextTick()
      panelEl.value?.focus()
    } else {
      if (scrollLocked) {
        unlockBodyScroll()
        scrollLocked = false
      }
      restoreFocus?.focus?.()
      restoreFocus = null
    }
  },
)

onMounted(() => document.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKeydown)
  if (scrollLocked) {
    unlockBodyScroll()
    scrollLocked = false
  }
})
</script>

<template>
  <Teleport to="body">
    <Transition name="ui-dlg">
      <div v-if="visible" class="ui-dialog__mask" @click.self="close">
        <div
          ref="panelEl"
          class="ui-dialog"
          role="dialog"
          aria-modal="true"
          tabindex="-1"
          :style="{ width: widthStyle }"
        >
          <header v-if="header" class="ui-dialog__head">
            <span class="ui-dialog__title">{{ header }}</span>
            <button
              type="button"
              class="ui-dialog__close"
              :aria-label="t('common.close')"
              @click="close"
            >
              ×
            </button>
          </header>

          <div class="ui-dialog__body">
            <slot />
          </div>

          <footer v-if="footer" class="ui-dialog__foot">
            <UiButton variant="outline" @click="close">
              {{ cancelBtn ?? t('common.cancel') }}
            </UiButton>
            <UiButton :theme="confirmTheme" :loading="confirmBtnLoading" @click="onConfirm">
              {{ confirmBtn?.content ?? t('common.confirm') }}
            </UiButton>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.ui-dialog__mask {
  position: fixed;
  inset: 0;
  z-index: 2500;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(0, 0, 0, 0.45);
}

.ui-dialog {
  display: flex;
  flex-direction: column;
  max-width: 100%;
  max-height: calc(100vh - 48px);
  border-radius: 12px;
  background: var(--mc-material);
  backdrop-filter: var(--mc-blur);
  box-shadow: var(--mc-elev-3);
  outline: none;
}

.ui-dialog__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 16px 18px 8px;
}

.ui-dialog__title {
  flex: 1;
  min-width: 0;
  font-size: var(--mc-fs-title);
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.ui-dialog__close {
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: var(--mc-radius-sm);
  background: transparent;
  color: var(--td-text-color-placeholder);
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
}

.ui-dialog__close:hover {
  background: var(--td-bg-color-container-hover);
  color: var(--td-text-color-primary);
}

.ui-dialog__body {
  padding: 8px 18px 4px;
  overflow: auto;
  color: var(--td-text-color-primary);
  font-size: 14px;
}

.ui-dialog__foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 16px 18px 16px;
}

.ui-dlg-enter-active,
.ui-dlg-leave-active {
  transition: opacity 140ms var(--mc-ease-in-out);
}

.ui-dlg-enter-active .ui-dialog,
.ui-dlg-leave-active .ui-dialog {
  transition: transform 140ms var(--mc-ease-in-out);
}

.ui-dlg-enter-from,
.ui-dlg-leave-to {
  opacity: 0;
}

.ui-dlg-enter-from .ui-dialog,
.ui-dlg-leave-to .ui-dialog {
  transform: scale(0.96) translateY(-6px);
}

@media (prefers-reduced-motion: reduce) {
  .ui-dlg-enter-active,
  .ui-dlg-leave-active,
  .ui-dlg-enter-active .ui-dialog,
  .ui-dlg-leave-active .ui-dialog {
    transition: none;
  }
}
</style>
