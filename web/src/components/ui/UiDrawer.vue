<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue'

import { useAppI18n } from '@/composables/useAppI18n'

/**
 * 右侧抽屉（UnoCSS 实现，替代 TDesign Drawer）。
 * 关闭途径：右上角 ×、点遮罩、Esc。
 */
const props = withDefaults(
  defineProps<{
    visible?: boolean
    header?: string
    /** 宽度，数字按 px 处理 */
    size?: string | number
  }>(),
  { size: 420 },
)

const emit = defineEmits<{ 'update:visible': [value: boolean] }>()
const { t } = useAppI18n()

const widthStyle = computed(() => (typeof props.size === 'number' ? `${props.size}px` : props.size))

function close() {
  emit('update:visible', false)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.visible) close()
}

onMounted(() => document.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => document.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <Transition name="ui-dw">
      <div v-if="visible" class="ui-drawer__mask" @click.self="close">
        <aside class="ui-drawer" role="dialog" aria-modal="true" :style="{ width: widthStyle }">
          <header class="ui-drawer__head">
            <span class="ui-drawer__title">{{ header }}</span>
            <button
              type="button"
              class="ui-drawer__close"
              :aria-label="t('common.close')"
              @click="close"
            >
              ×
            </button>
          </header>
          <div class="ui-drawer__body">
            <slot />
          </div>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.ui-drawer__mask {
  position: fixed;
  inset: 0;
  z-index: 2500;
  display: flex;
  justify-content: flex-end;
  background: rgba(0, 0, 0, 0.45);
}

.ui-drawer {
  display: flex;
  flex-direction: column;
  height: 100%;
  max-width: 100vw;
  background: var(--mc-material);
  backdrop-filter: var(--mc-blur);
  box-shadow: var(--mc-elev-3);
}

.ui-drawer__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 16px 16px;
  border-bottom: 1px solid var(--mc-hairline);
}

.ui-drawer__title {
  flex: 1;
  min-width: 0;
  font-size: var(--mc-fs-title);
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.ui-drawer__close {
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

.ui-drawer__close:hover {
  background: var(--td-bg-color-container-hover);
  color: var(--td-text-color-primary);
}

.ui-drawer__body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 16px 16px;
}

.ui-dw-enter-active,
.ui-dw-leave-active {
  transition: opacity 160ms var(--mc-ease-in-out);
}

.ui-dw-enter-active .ui-drawer,
.ui-dw-leave-active .ui-drawer {
  transition: transform 160ms cubic-bezier(0.22, 1, 0.36, 1);
}

.ui-dw-enter-from,
.ui-dw-leave-to {
  opacity: 0;
}

.ui-dw-enter-from .ui-drawer,
.ui-dw-leave-to .ui-drawer {
  transform: translateX(16px);
}

@media (prefers-reduced-motion: reduce) {
  .ui-dw-enter-active,
  .ui-dw-leave-active,
  .ui-dw-enter-active .ui-drawer,
  .ui-dw-leave-active .ui-drawer {
    transition: none;
  }
}
</style>
