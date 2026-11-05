<script setup lang="ts">
import { computed, nextTick, ref, useId } from 'vue'

import UiPopup from './UiPopup.vue'

export interface UiSelectOption {
  label: string
  value: string | number
  disabled?: boolean
}

/**
 * 下拉选择（UnoCSS 实现，替代 TDesign Select）。
 * 面板复用 UiPopup，因此点外部/Esc/滚动自动关闭。
 */
const props = withDefaults(
  defineProps<{
    modelValue?: string | number
    options?: UiSelectOption[]
    placeholder?: string
    clearable?: boolean
    disabled?: boolean
    size?: 'small' | 'medium'
  }>(),
  { modelValue: '', options: () => [], clearable: false, disabled: false, size: 'medium' },
)

const emit = defineEmits<{
  'update:modelValue': [value: string | number]
  change: [value: string | number]
}>()

const open = ref(false)

const selected = computed(() => props.options.find((o) => o.value === props.modelValue))
const hasValue = computed(() => props.modelValue !== '' && props.modelValue !== undefined)

const triggerCls = computed(() => [
  'flex items-center gap-1.5 w-full rounded-md border-1 border-solid bg-surface transition-colors cursor-pointer',
  props.size === 'small' ? 'h-7 px-2 text-[12px]' : 'h-8 px-2.5 text-[13px]',
  props.disabled ? 'border-line opacity-60 cursor-not-allowed' : 'border-line hover:border-brand-line-strong',
  open.value ? 'border-brand' : '',
])

function pick(o: UiSelectOption, close: () => void) {
  if (o.disabled) return
  emit('update:modelValue', o.value)
  emit('change', o.value)
  close()
}

function clear() {
  emit('update:modelValue', '')
  emit('change', '')
}

function toggle() {
  if (props.disabled) return
  open.value = !open.value
}

// ===== 键盘操作（读屏与纯键盘用户依赖它选中选项） =====

const listId = `ui-select${useId()}`
const activeIndex = ref(-1)

const optionId = (i: number) => `${listId}-opt-${i}`
/** 可选（非 disabled）选项下标 */
const enabledIndexes = computed(() =>
  props.options.map((o, i) => (o.disabled ? -1 : i)).filter((i) => i >= 0),
)

function openPanel() {
  if (props.disabled) return
  open.value = true
  const current = props.options.findIndex((o) => o.value === props.modelValue && !o.disabled)
  activeIndex.value = current >= 0 ? current : (enabledIndexes.value[0] ?? -1)
}

function moveActive(delta: number) {
  const list = enabledIndexes.value
  if (!list.length) return
  const pos = list.indexOf(activeIndex.value)
  const next =
    pos < 0
      ? delta > 0
        ? list[0]!
        : list[list.length - 1]!
      : list[(pos + delta + list.length) % list.length]!
  activeIndex.value = next
  void nextTick(() => document.getElementById(optionId(next))?.scrollIntoView({ block: 'nearest' }))
}

function onTriggerKeydown(e: KeyboardEvent) {
  if (props.disabled) return
  if (!open.value) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      openPanel()
    }
    return
  }
  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault()
      moveActive(1)
      break
    case 'ArrowUp':
      e.preventDefault()
      moveActive(-1)
      break
    case 'Home':
      e.preventDefault()
      activeIndex.value = enabledIndexes.value[0] ?? -1
      break
    case 'End':
      e.preventDefault()
      activeIndex.value = enabledIndexes.value[enabledIndexes.value.length - 1] ?? -1
      break
    case 'Enter':
    case ' ': {
      e.preventDefault()
      const o = props.options[activeIndex.value]
      if (o) pick(o, () => (open.value = false))
      break
    }
    case 'Escape':
      e.preventDefault()
      open.value = false
      break
    case 'Tab':
      // 选项面板不可聚焦，Tab 直接收起，焦点继续按自然顺序走
      open.value = false
      break
  }
}
</script>

<template>
  <UiPopup v-model:visible="open" placement="bottom-left" :min-width="160">
    <button
      type="button"
      :class="triggerCls"
      :disabled="disabled"
      role="combobox"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="listId"
      :aria-activedescendant="open && activeIndex >= 0 ? optionId(activeIndex) : undefined"
      @click="toggle"
      @keydown="onTriggerKeydown"
    >
      <span class="flex-1 min-w-0 truncate text-left" :class="hasValue ? 'text-ink' : 'text-ink-3'">
        {{ selected?.label ?? (hasValue ? String(modelValue) : placeholder) }}
      </span>
      <!-- 清除按钮用 span：button 套 button 是无效 HTML，且会让读屏把两个控件念成一个 -->
      <span
        v-if="clearable && hasValue"
        role="button"
        tabindex="0"
        class="flex-shrink-0 w-4 h-4 flex items-center justify-center rounded-full bg-transparent text-ink-3 hover:text-ink hover:bg-hover"
        aria-label="clear"
        @click.stop="clear"
        @keydown.enter.prevent.stop="clear"
        @keydown.space.prevent.stop="clear"
      >
        <span class="text-[13px] leading-none">×</span>
      </span>
      <span v-else class="flex-shrink-0 text-[10px] text-ink-3">▾</span>
    </button>

    <template #content="{ close }">
      <ul :id="listId" role="listbox" class="m-0 p-1 list-none max-h-[240px] overflow-auto">
        <li v-if="!options.length" class="px-2.5 py-2 text-[12px] text-ink-3">
          <slot name="empty">—</slot>
        </li>
        <li
          v-for="(o, i) in options"
          :id="optionId(i)"
          :key="String(o.value)"
          role="option"
          :aria-selected="o.value === modelValue"
          :aria-disabled="o.disabled || undefined"
          class="px-2.5 py-1.5 rounded-md text-[13px] cursor-pointer transition-colors"
          :class="[
            o.disabled ? 'text-ink-4 cursor-not-allowed' : 'text-ink hover:bg-hover',
            o.value === modelValue ? 'text-brand font-medium' : '',
            open && i === activeIndex ? 'bg-hover' : '',
          ]"
          @click="pick(o, close)"
        >
          {{ o.label }}
        </li>
      </ul>
    </template>
  </UiPopup>
</template>
