<script setup lang="ts">
import { computed } from 'vue'

/**
 * 基础按钮（UnoCSS 原子类实现，用于替代 TDesign Button）。
 *
 * 颜色全部走 uno theme 里的语义 token（brand / ink / line / danger…），
 * 它们指向 --td-* 变量，因此亮暗主题自动跟随，不需要写两套样式。
 */
const props = withDefaults(
  defineProps<{
    theme?: 'default' | 'primary' | 'danger' | 'success'
    variant?: 'base' | 'outline' | 'text'
    size?: 'small' | 'medium' | 'large'
    block?: boolean
    loading?: boolean
    disabled?: boolean
    type?: 'button' | 'submit'
  }>(),
  {
    theme: 'default',
    variant: 'base',
    size: 'medium',
    block: false,
    loading: false,
    disabled: false,
    type: 'button',
  },
)

const emit = defineEmits<{ click: [e: MouseEvent] }>()

const sizeCls = computed(() => {
  switch (props.size) {
    case 'small':
      return 'h-7 px-2.5 text-[13px] gap-1 rounded-sm'
    case 'large':
      return 'h-10 px-4 text-[15px] gap-2 rounded-md'
    default:
      return 'h-8 px-3 text-[13.5px] gap-1.5 rounded-md'
  }
})

const themeCls = computed(() => {
  // 文字按钮：无底无边，靠品牌色区分
  if (props.variant === 'text') {
    if (props.theme === 'danger') return 'text-danger hover:bg-danger/10'
    if (props.theme === 'primary') return 'text-brand hover:bg-brand-light'
    return 'text-ink-2 hover:text-ink hover:bg-hover'
  }
  // 描边：白底 + 细边
  if (props.variant === 'outline') {
    if (props.theme === 'danger') return 'border border-danger/40 text-danger hover:bg-danger/8'
    if (props.theme === 'primary') return 'border border-brand/40 text-brand hover:bg-brand-light'
    return 'border border-line text-ink hover:bg-hover'
  }
  // 实心
  switch (props.theme) {
    case 'primary':
      return 'bg-brand text-white hover:bg-brand-hover active:bg-brand-active'
    case 'danger':
      return 'bg-danger text-white hover:opacity-90 active:opacity-80'
    case 'success':
      return 'bg-success text-white hover:opacity-90'
    default:
      return 'bg-hover text-ink hover:bg-active'
  }
})

const cls = computed(() => [
  'inline-flex items-center justify-center select-none whitespace-nowrap',
  'font-[inherit] transition-colors duration-150 outline-none',
  'focus-visible:ring-2 focus-visible:ring-brand/50',
  sizeCls.value,
  themeCls.value,
  props.block ? 'w-full' : '',
  props.disabled || props.loading ? 'opacity-50 cursor-not-allowed pointer-events-none' : 'cursor-pointer',
])

function onClick(e: MouseEvent) {
  if (props.disabled || props.loading) return
  emit('click', e)
}
</script>

<template>
  <button :type="type" :class="cls" :disabled="disabled || loading" @click="onClick">
    <span v-if="loading" class="ui-btn__spin" aria-hidden="true" />
    <slot />
  </button>
</template>

<style scoped>
.ui-btn__spin {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 1.5px solid currentcolor;
  border-top-color: transparent;
  animation: ui-btn-spin 700ms linear infinite;
  flex-shrink: 0;
}

@keyframes ui-btn-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (prefers-reduced-motion: reduce) {
  .ui-btn__spin {
    animation-duration: 2s;
  }
}
</style>
