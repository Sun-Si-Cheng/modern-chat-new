<script setup lang="ts">
import { computed } from 'vue'

/** 开关（UnoCSS 实现，替代 TDesign Switch） */
const props = withDefaults(
  defineProps<{
    value?: boolean
    disabled?: boolean
    size?: 'small' | 'medium'
  }>(),
  { value: false, disabled: false, size: 'medium' },
)

const emit = defineEmits<{ change: [value: boolean] }>()

const trackCls = computed(() => [
  'relative inline-flex items-center flex-shrink-0 rounded-full border-none transition-colors duration-150',
  props.size === 'small' ? 'w-8 h-[18px]' : 'w-10 h-[22px]',
  props.value ? 'bg-brand' : 'bg-active',
  props.disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer',
])

const knobCls = computed(() => [
  'absolute rounded-full bg-white shadow-sm transition-transform duration-150',
  props.size === 'small' ? 'w-3.5 h-3.5' : 'w-[18px] h-[18px]',
  props.size === 'small'
    ? props.value
      ? 'translate-x-[15px]'
      : 'translate-x-[2px]'
    : props.value
      ? 'translate-x-[19px]'
      : 'translate-x-[2px]',
])

function toggle() {
  if (props.disabled) return
  emit('change', !props.value)
}
</script>

<template>
  <button
    type="button"
    role="switch"
    :aria-checked="value"
    :disabled="disabled"
    :class="trackCls"
    @click="toggle"
  >
    <span :class="knobCls" />
  </button>
</template>
