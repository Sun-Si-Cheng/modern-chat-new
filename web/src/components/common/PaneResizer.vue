<script setup lang="ts">
import { ref } from 'vue'

/**
 * 区域宽度拖拽条。
 * - 拖动改宽度（右侧面板用 invert，向左拖变宽）
 * - 双击触发 dblclick（用于在「常驻 / 抽屉」之间切换）
 * - 键盘可访问：聚焦后用 ←/→ 调整（右侧面板方向相反）
 */
const props = defineProps<{
  modelValue: number
  min: number
  max: number
  /** true = 分隔条在区域左侧（如右侧详情面板），拖动方向相反 */
  invert?: boolean
  label: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: number]
  dblclick: []
}>()

const dragging = ref(false)

function clamp(v: number) {
  return Math.min(props.max, Math.max(props.min, Math.round(v)))
}

function onPointerDown(e: PointerEvent) {
  if (e.button !== 0) return
  e.preventDefault()
  const startX = e.clientX
  const startW = props.modelValue
  dragging.value = true
  document.body.style.cursor = 'col-resize'
  // 拖拽期间禁止选中文本，否则会一路把页面文字刷蓝
  document.body.style.userSelect = 'none'

  const move = (ev: PointerEvent) => {
    const delta = props.invert ? startX - ev.clientX : ev.clientX - startX
    emit('update:modelValue', clamp(startW + delta))
  }
  const up = () => {
    dragging.value = false
    document.body.style.cursor = ''
    document.body.style.userSelect = ''
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', up)
    window.removeEventListener('pointercancel', up)
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up)
  window.addEventListener('pointercancel', up)
}

function onKeydown(e: KeyboardEvent) {
  const step = e.shiftKey ? 24 : 8
  if (e.key === 'ArrowLeft') {
    e.preventDefault()
    emit('update:modelValue', clamp(props.modelValue + (props.invert ? step : -step)))
  } else if (e.key === 'ArrowRight') {
    e.preventDefault()
    emit('update:modelValue', clamp(props.modelValue + (props.invert ? -step : step)))
  }
}
</script>

<template>
  <div
    class="mc-resizer"
    :class="{ 'is-dragging': dragging }"
    role="separator"
    aria-orientation="vertical"
    tabindex="0"
    :aria-label="label"
    :aria-valuenow="modelValue"
    :aria-valuemin="min"
    :aria-valuemax="max"
    @pointerdown="onPointerDown"
    @dblclick="emit('dblclick')"
    @keydown="onKeydown"
  >
    <span class="mc-resizer__grip" aria-hidden="true" />
  </div>
</template>

<style scoped>
.mc-resizer {
  position: relative;
  flex: 0 0 6px;
  width: 6px;
  cursor: col-resize;
  background: transparent;
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 5;
  /* 扩大命中区域（视觉上仍是细条） */
  touch-action: none;
}

.mc-resizer::before {
  content: '';
  position: absolute;
  inset: 0 -3px;
}

.mc-resizer__grip {
  width: 2px;
  height: 28px;
  border-radius: 2px;
  background: var(--mc-hairline);
  opacity: 0;
  transition: opacity var(--mc-dur-fast, 120ms) ease;
}

.mc-resizer:hover .mc-resizer__grip,
.mc-resizer:focus-visible .mc-resizer__grip,
.mc-resizer.is-dragging .mc-resizer__grip {
  opacity: 1;
  background: var(--td-brand-color);
}

.mc-resizer:focus-visible {
  outline: none;
}

.mc-resizer:focus-visible::after {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: 3px;
  box-shadow: inset 0 0 0 1px var(--td-brand-color);
}
</style>
