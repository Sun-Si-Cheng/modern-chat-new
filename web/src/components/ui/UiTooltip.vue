<script setup lang="ts">
import { ref } from 'vue'

/**
 * 文字提示（UnoCSS 实现，替代 TDesign Tooltip）。
 * hover / focus 时在元素上方居中显示；纯展示，不接收鼠标事件。
 */
withDefaults(defineProps<{ content?: string; disabled?: boolean }>(), { disabled: false })

const anchorEl = ref<HTMLElement | null>(null)
const show = ref(false)
const pos = ref({ left: 0, top: 0 })

function place() {
  const r = anchorEl.value?.getBoundingClientRect()
  if (!r) return
  pos.value = { left: r.left + r.width / 2, top: r.top - 8 }
}

function open() {
  place()
  show.value = true
}

function close() {
  show.value = false
}
</script>

<template>
  <span
    ref="anchorEl"
    class="inline-flex items-center"
    @mouseenter="open"
    @mouseleave="close"
    @focusin="open"
    @focusout="close"
  >
    <slot />
  </span>

  <Teleport to="body">
    <Transition name="ui-tip">
      <div
        v-if="show && content && !disabled"
        class="ui-tooltip"
        role="tooltip"
        :style="{ left: `${pos.left}px`, top: `${pos.top}px` }"
      >
        {{ content }}
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.ui-tooltip {
  position: fixed;
  z-index: 3000;
  transform: translate(-50%, -100%);
  max-width: 260px;
  padding: 4px 8px;
  border-radius: var(--mc-radius-sm);
  background: rgba(24, 24, 26, 0.94);
  color: #fff;
  font-size: 12px;
  line-height: 1.4;
  white-space: nowrap;
  pointer-events: none;
  box-shadow: 0 6px 18px -8px rgba(0, 0, 0, 0.5);
}

.ui-tip-enter-active,
.ui-tip-leave-active {
  transition: opacity 100ms var(--mc-ease-in-out);
}

.ui-tip-enter-from,
.ui-tip-leave-to {
  opacity: 0;
}
</style>
