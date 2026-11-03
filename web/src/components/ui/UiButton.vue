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
    /** square/circle：等宽高图标按钮（兼容 TDesign 的 shape 属性） */
    shape?: 'default' | 'square' | 'circle'
  }>(),
  {
    theme: 'default',
    variant: 'base',
    size: 'medium',
    block: false,
    loading: false,
    disabled: false,
    type: 'button',
    shape: 'default',
  },
)

const emit = defineEmits<{ click: [e: MouseEvent] }>()

const sizeCls = computed(() => {
  // square/circle 是等宽高图标按钮，不能带水平内边距
  const sq = props.shape !== 'default'
  const round = props.shape === 'circle' ? 'rounded-full' : ''
  if (props.size === 'small') return `h-7 text-[13px] gap-1 ${sq ? 'w-7 p-0' : 'px-2.5'} ${round || 'rounded-sm'}`
  if (props.size === 'large') return `h-10 text-[15px] gap-2 ${sq ? 'w-10 p-0' : 'px-4'} ${round || 'rounded-md'}`
  return `h-8 text-[13.5px] gap-1.5 ${sq ? 'w-8 p-0' : 'px-3'} ${round || 'rounded-md'}`
})

const themeCls = computed(() => {
  // 每个分支都显式声明 background：
  // <button> 自带浏览器默认灰底（rgb(240,240,240)），不声明就会漏出来
  if (props.variant === 'text') {
    if (props.theme === 'danger') return 'bg-transparent text-danger hover:bg-danger/10'
    if (props.theme === 'primary') return 'bg-transparent text-brand hover:bg-brand-light'
    return 'bg-transparent text-ink-2 hover:text-ink hover:bg-hover'
  }
  // 描边：透明底 + 细边
  // 必须显式 border-solid + border-1：项目没有引入 preflight，
  // 只写 `border` 会因浏览器默认 border-style:none 而部分边不显示。
  if (props.variant === 'outline') {
    if (props.theme === 'danger')
      return 'bg-transparent border-1 border-solid border-danger/40 text-danger hover:bg-danger/8'
    if (props.theme === 'primary')
      return 'bg-transparent border-1 border-solid border-brand/40 text-brand hover:bg-brand-light'
    return 'bg-transparent border-1 border-solid border-line text-ink hover:bg-hover'
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
    <!-- 兼容 TDesign 的 #icon 插槽（QuickPrefs 等图标按钮在用） -->
    <slot v-else name="icon" />
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
