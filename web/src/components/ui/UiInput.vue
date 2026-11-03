<script setup lang="ts">
import { computed } from 'vue'

/**
 * 基础输入框（UnoCSS 实现，替代 TDesign Input）。
 * 焦点态用 `focus-within` 把边框染成品牌色，与存量表单观感一致。
 */
const props = withDefaults(
  defineProps<{
    modelValue?: string
    placeholder?: string
    clearable?: boolean
    disabled?: boolean
    size?: 'small' | 'medium'
    type?: 'text' | 'password' | 'email'
    maxlength?: number
  }>(),
  { modelValue: '', clearable: false, disabled: false, size: 'medium', type: 'text' },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  enter: []
  change: [value: string]
  clear: []
}>()

const wrapCls = computed(() => [
  'flex items-center gap-1 w-full rounded-md border bg-surface transition-colors',
  props.size === 'small' ? 'h-7 px-2' : 'h-8 px-2.5',
  props.disabled
    ? 'border-line opacity-60 cursor-not-allowed'
    : 'border-line hover:border-brand/50 focus-within:border-brand',
])

const inputCls = computed(() => [
  'flex-1 min-w-0 bg-transparent outline-none border-none',
  props.size === 'small' ? 'text-[12.5px]' : 'text-[13.5px]',
  'text-ink placeholder:text-ink-3',
])

function onInput(e: Event) {
  emit('update:modelValue', (e.target as HTMLInputElement).value)
}

function onChange(e: Event) {
  emit('change', (e.target as HTMLInputElement).value)
}

function clear() {
  emit('update:modelValue', '')
  emit('change', '')
  emit('clear')
}
</script>

<template>
  <div :class="wrapCls">
    <input
      :value="modelValue"
      :type="type"
      :placeholder="placeholder"
      :disabled="disabled"
      :maxlength="maxlength"
      :class="inputCls"
      @input="onInput"
      @change="onChange"
      @keydown.enter.prevent="emit('enter')"
    />
    <button
      v-if="clearable && modelValue && !disabled"
      type="button"
      class="flex-shrink-0 w-4 h-4 flex items-center justify-center rounded-full text-ink-3 hover:text-ink hover:bg-hover"
      aria-label="clear"
      @click="clear"
    >
      <span class="text-[13px] leading-none">×</span>
    </button>
  </div>
</template>
