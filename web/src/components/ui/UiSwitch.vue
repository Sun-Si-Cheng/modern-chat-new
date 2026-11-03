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
  // 关闭态用 ink-4 而不是 bg-active：后者在暗色下与背景几乎同色，
  // 只看得到白色圆点、看不出这是一颗开关
  props.value ? 'bg-brand' : 'bg-ink-4',
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
