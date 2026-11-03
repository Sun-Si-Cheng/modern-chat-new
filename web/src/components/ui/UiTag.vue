<script setup lang="ts">
import { computed } from 'vue'

/** 小标签（UnoCSS 实现，替代 TDesign Tag） */
const props = withDefaults(
  defineProps<{
    theme?: 'default' | 'primary' | 'success' | 'warning' | 'danger'
    variant?: 'light' | 'outline' | 'dark'
    size?: 'small' | 'medium'
  }>(),
  { theme: 'default', variant: 'light', size: 'small' },
)

const cls = computed(() => {
  const size = props.size === 'small' ? 'h-[18px] px-1.5 text-[11px]' : 'h-6 px-2 text-[12px]'
  const map: Record<string, string> = {
    'default-light': 'bg-hover text-ink-2',
    'default-outline': 'border border-line text-ink-2',
    'default-dark': 'bg-ink-2 text-white',
    'primary-light': 'bg-brand-light text-brand',
    'primary-outline': 'border border-brand-line text-brand',
    'primary-dark': 'bg-brand text-white',
    'success-light': 'bg-success-soft text-success',
    'success-outline': 'border border-success-line text-success',
    'success-dark': 'bg-success text-white',
    'warning-light': 'bg-warning-soft text-warning',
    'warning-outline': 'border border-warning-line text-warning',
    'warning-dark': 'bg-warning text-white',
    'danger-light': 'bg-danger-soft text-danger',
    'danger-outline': 'border border-danger-line text-danger',
    'danger-dark': 'bg-danger text-white',
  }
  const key = `${props.theme}-${props.variant}`
  return [
    'inline-flex items-center justify-center rounded-sm whitespace-nowrap leading-none',
    size,
    map[key] ?? map['default-light'],
  ]
})
</script>

<template>
  <span :class="cls"><slot /></span>
</template>
