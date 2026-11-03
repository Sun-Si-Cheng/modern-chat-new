<script setup lang="ts">
import { computed, ref } from 'vue'

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
  props.size === 'small' ? 'h-7 px-2 text-[12.5px]' : 'h-8 px-2.5 text-[13.5px]',
  props.disabled ? 'border-line opacity-60 cursor-not-allowed' : 'border-line hover:border-brand/50',
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
</script>

<template>
  <UiPopup v-model:visible="open" placement="bottom-left" :min-width="160">
    <button type="button" :class="triggerCls" :disabled="disabled" @click="toggle">
      <span class="flex-1 min-w-0 truncate text-left" :class="hasValue ? 'text-ink' : 'text-ink-3'">
        {{ selected?.label ?? (hasValue ? String(modelValue) : placeholder) }}
      </span>
      <button
        v-if="clearable && hasValue"
        type="button"
        class="flex-shrink-0 w-4 h-4 flex items-center justify-center rounded-full bg-transparent text-ink-3 hover:text-ink hover:bg-hover"
        aria-label="clear"
        @click.stop="clear"
      >
        <span class="text-[13px] leading-none">×</span>
      </button>
      <span v-else class="flex-shrink-0 text-[10px] text-ink-3">▾</span>
    </button>

    <template #content="{ close }">
      <ul class="m-0 p-1 list-none max-h-[240px] overflow-auto">
        <li v-if="!options.length" class="px-2.5 py-2 text-[12.5px] text-ink-3">
          <slot name="empty">—</slot>
        </li>
        <li
          v-for="o in options"
          :key="String(o.value)"
          class="px-2.5 py-1.5 rounded-md text-[13px] cursor-pointer transition-colors"
          :class="[
            o.disabled ? 'text-ink-4 cursor-not-allowed' : 'text-ink hover:bg-hover',
            o.value === modelValue ? 'text-brand font-medium' : '',
          ]"
          @click="pick(o, close)"
        >
          {{ o.label }}
        </li>
      </ul>
    </template>
  </UiPopup>
</template>
