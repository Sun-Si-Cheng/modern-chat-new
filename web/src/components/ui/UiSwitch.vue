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
  'relative inline-flex items-center flex-shrink-0 rounded-full border-none overflow-hidden',
  // overflow-hidden：滑钮位移走 spring 曲线（有 ~10% 超调），
  // 不裁剪的话圆圈在动画峰值会越过轨道圆头端盖，看起来「滑出滑道」
  props.size === 'small' ? 'w-8 h-[18px]' : 'w-10 h-[22px]',
  // 关闭态用 ink-4 而不是 bg-active：后者在暗色下与背景几乎同色，
  // 只看得到白色圆点、看不出这是一颗开关
  props.value ? 'bg-brand' : 'bg-ink-4',
  props.disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer',
])

const knobCls = computed(() => [
  // 显式 top/left 定位：不依赖「flex 容器内 absolute 子元素的静态位置」，
  // 各浏览器对该位置的处理不一致，曾导致圆圈垂直不居中。
  // 两种尺寸下 (轨道高-滑钮直径)/2 都是 2px，top/left 统一 2px。
  // 开状态位移 = 轨道宽 - 滑钮 - 2×2px（两端留白对称），medium 18px / small 14px。
  'ui-switch__knob absolute top-[2px] left-[2px] rounded-full bg-white shadow-sm',
  props.size === 'small' ? 'w-3.5 h-3.5' : 'w-[18px] h-[18px]',
  props.size === 'small'
    ? props.value
      ? 'translate-x-[14px]'
      : 'translate-x-0'
    : props.value
      ? 'translate-x-[18px]'
      : 'translate-x-0',
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
    class="ui-switch__track"
    @click="toggle"
  >
    <span :class="knobCls" />
  </button>
</template>

<style scoped>
/* 轨道：颜色变化走对称曲线（只变颜色，不需要方向感），按压内缩走 ease-out */
.ui-switch__track {
  transition:
    background-color 180ms var(--mc-ease-in-out),
    transform 100ms var(--mc-ease-out);
}

/* 按压：整颗轻微内缩 */
.ui-switch__track:active:not(:disabled) {
  transform: scale(0.96);
}

/* 滑钮：位移走弹性曲线 —— 开关是「按下去弹过来」的物理动作 */
.ui-switch__knob {
  transition: transform var(--mc-dur-base) var(--mc-ease-spring);
}

@media (prefers-reduced-motion: reduce) {
  .ui-switch__knob {
    transition-duration: 1ms;
  }
}
</style>
