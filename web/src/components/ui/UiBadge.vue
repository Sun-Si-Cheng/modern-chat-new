<script setup lang="ts">
import { computed } from 'vue'

/** 未读/计数红点（UnoCSS 实现，替代 TDesign Badge） */
const props = withDefaults(
  defineProps<{
    count?: number
    /** 只显示小圆点，不显示数字 */
    dot?: boolean
    max?: number
    /** 兼容 TDesign 的 size 属性（当前统一一种尺寸，接收后忽略） */
    size?: 'small' | 'medium'
  }>(),
  { count: 0, dot: false, max: 99 },
)

const text = computed(() => (props.count > props.max ? `${props.max}+` : String(props.count)))
const show = computed(() => props.dot || props.count > 0)
</script>

<template>
  <span
    v-if="show"
    :class="[
      'inline-flex items-center justify-center flex-shrink-0',
      dot
        ? 'w-2 h-2 rounded-full bg-danger'
        : 'min-w-[18px] h-[18px] px-1 rounded-full bg-danger text-white text-[11px] leading-none font-medium',
    ]"
    :aria-label="dot ? undefined : text"
  >
    {{ dot ? '' : text }}
  </span>
</template>
