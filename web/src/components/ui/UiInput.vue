<script setup lang="ts">
import { computed, ref } from 'vue'

/**
 * 基础输入框（UnoCSS 实现，替代 TDesign Input）。
 *
 * 兼容 TDesign 的常用能力，方便逐文件替换：
 * - `#prefix-icon` 插槽（前置图标）
 * - `size`: small / medium / large
 * - 通过 ref 调用 `focus()`（AddFriendDialog 打开时需要自动聚焦）
 */
const props = withDefaults(
  defineProps<{
    modelValue?: string
    placeholder?: string
    clearable?: boolean
    disabled?: boolean
    size?: 'small' | 'medium' | 'large'
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

const inputEl = ref<HTMLInputElement | null>(null)
defineExpose({ focus: () => inputEl.value?.focus() })

const wrapCls = computed(() => [
  'flex items-center gap-1.5 w-full rounded-md border bg-surface transition-colors',
  props.size === 'small' ? 'h-7 px-2' : props.size === 'large' ? 'h-10 px-3' : 'h-8 px-2.5',
  props.disabled
    ? 'border-line opacity-60 cursor-not-allowed'
    : 'border-line hover:border-brand-line-strong focus-within:border-brand',
])

const inputCls = computed(() => [
  'flex-1 min-w-0 bg-transparent outline-none border-none',
  props.size === 'small' ? 'text-[12px]' : props.size === 'large' ? 'text-[16px]' : 'text-[13px]',
  'text-ink placeholder:text-ink-3',
])

function onInput(e: Event) {
  emit('update:modelValue', (e.target as HTMLInputElement).value)
}

/**
 * 回车触发 enter 事件。
 * 中文输入法选词时按回车只是上屏，不应触发提交（搜索 / 公告都会误发）。
 */
function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Enter' || e.isComposing || e.keyCode === 229) return
  e.preventDefault()
  emit('enter')
}

function onChange(e: Event) {
  emit('change', (e.target as HTMLInputElement).value)
}

function clear() {
  emit('update:modelValue', '')
  emit('change', '')
  emit('clear')
  inputEl.value?.focus()
}
</script>

<template>
  <div :class="wrapCls">
    <span v-if="$slots['prefix-icon']" class="flex-shrink-0 flex items-center text-ink-3">
      <slot name="prefix-icon" />
    </span>
    <input
      ref="inputEl"
      :value="modelValue"
      :type="type"
      :placeholder="placeholder"
      :disabled="disabled"
      :maxlength="maxlength"
      :class="inputCls"
      @input="onInput"
      @change="onChange"
      @keydown.enter="onKeydown"
    />
    <button
      v-if="clearable && modelValue && !disabled"
      type="button"
      class="flex-shrink-0 w-4 h-4 flex items-center justify-center rounded-full border-none bg-transparent text-ink-3 hover:text-ink hover:bg-hover"
      aria-label="clear"
      @click="clear"
    >
      <span class="text-[13px] leading-none">×</span>
    </button>
  </div>
</template>
