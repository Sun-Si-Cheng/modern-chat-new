<script setup lang="ts">
import { computed, ref, watch } from 'vue'

/** 数字输入（右侧上下步进，替代 TDesign InputNumber 的 column 形态） */
const props = withDefaults(
  defineProps<{
    modelValue?: number
    min?: number
    max?: number
    step?: number
    suffix?: string
    disabled?: boolean
  }>(),
  { modelValue: 0, min: -Infinity, max: Infinity, step: 1, disabled: false },
)

const emit = defineEmits<{
  'update:modelValue': [value: number]
  change: [value: number]
}>()

const draft = ref(String(props.modelValue ?? 0))

watch(
  () => props.modelValue,
  (v) => {
    draft.value = String(v ?? 0)
  },
)

const clamp = (v: number) => Math.min(props.max, Math.max(props.min, v))

function commit(v: number) {
  const next = clamp(Number.isFinite(v) ? v : 0)
  draft.value = String(next)
  emit('update:modelValue', next)
  emit('change', next)
}

function onInput(e: Event) {
  draft.value = (e.target as HTMLInputElement).value
}

function onBlur() {
  const n = Number(draft.value)
  commit(Number.isFinite(n) ? n : props.min === -Infinity ? 0 : props.min)
}

function bump(dir: 1 | -1) {
  if (props.disabled) return
  commit((props.modelValue ?? 0) + dir * props.step)
}

const wrapCls = computed(() => [
  'flex items-center gap-1 w-full rounded-md border bg-surface transition-colors',
  'h-8 px-2.5',
  props.disabled ? 'border-line opacity-60' : 'border-line hover:border-brand-line-strong focus-within:border-brand',
])
</script>

<template>
  <div :class="wrapCls">
    <input
      :value="draft"
      type="text"
      inputmode="numeric"
      :disabled="disabled"
      class="flex-1 min-w-0 bg-transparent outline-none border-none text-[13.5px] text-ink"
      @input="onInput"
      @blur="onBlur"
      @keydown.enter="onBlur"
    />
    <span v-if="suffix" class="flex-shrink-0 text-[12px] text-ink-3">{{ suffix }}</span>
    <span class="flex-shrink-0 flex flex-col -my-1">
      <button
        type="button"
        class="w-4 h-3 flex items-center justify-center border-none bg-transparent text-ink-3 hover:text-brand"
        :disabled="disabled"
        aria-label="increase"
        @click="bump(1)"
      >
        <span class="text-[8px] leading-none">▲</span>
      </button>
      <button
        type="button"
        class="w-4 h-3 flex items-center justify-center border-none bg-transparent text-ink-3 hover:text-brand"
        :disabled="disabled"
        aria-label="decrease"
        @click="bump(-1)"
      >
        <span class="text-[8px] leading-none">▼</span>
      </button>
    </span>
  </div>
</template>
