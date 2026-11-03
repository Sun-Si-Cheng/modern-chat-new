<script setup lang="ts">
import { computed } from 'vue'

/** 复选框（UnoCSS 实现，替代 TDesign Checkbox） */
const props = withDefaults(
  defineProps<{
    modelValue?: boolean
    disabled?: boolean
    size?: 'small' | 'medium'
  }>(),
  { modelValue: false, disabled: false, size: 'medium' },
)

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  change: [value: boolean]
}>()

const boxCls = computed(() => [
  'inline-flex items-center justify-center flex-shrink-0 rounded-[4px] border transition-colors',
  props.size === 'small' ? 'w-3.5 h-3.5' : 'w-4 h-4',
  props.modelValue ? 'bg-brand border-brand text-white' : 'bg-transparent border-line text-transparent',
  props.disabled ? 'opacity-50' : '',
])

function onChange(e: Event) {
  const v = (e.target as HTMLInputElement).checked
  emit('update:modelValue', v)
  emit('change', v)
}
</script>

<template>
  <label
    class="inline-flex items-start gap-2 select-none"
    :class="disabled ? 'cursor-not-allowed opacity-70' : 'cursor-pointer'"
  >
    <span :class="boxCls">
      <svg v-if="modelValue" width="10" height="10" viewBox="0 0 12 12" fill="none" aria-hidden="true">
        <path
          d="M2.5 6.2 4.8 8.5 9.5 3.8"
          stroke="currentColor"
          stroke-width="1.8"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
      </svg>
    </span>
    <input
      type="checkbox"
      class="sr-only"
      :checked="modelValue"
      :disabled="disabled"
      @change="onChange"
    />
    <slot />
  </label>
</template>
