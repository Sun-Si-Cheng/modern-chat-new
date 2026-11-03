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
  'relative inline-flex items-center flex-shrink-0 rounded-full border-none',
  props.size === 'small' ? 'w-8 h-[18px]' : 'w-10 h-[22px]',
  // 关闭态用 ink-4 而不是 bg-active：后者在暗色下与背景几乎同色，
  // 只看得到白色圆点、看不出这是一颗开关
  props.value ? 'bg-brand' : 'bg-ink-4',
  props.disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer',
])

const knobCls = computed(() => [
  'ui-switch__knob absolute rounded-full bg-white shadow-sm',
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
    class="ui-switch__track"
    @click="toggle"
  >
    <span :class="knobCls" />
  </button>
</template>

<style scoped>
/* 轨道：颜色变化走对称曲线（只变颜色，不需要方向感） */
.ui-switch__track {
  transition: background-color 180ms var(--mc-ease-in-out);
}

/* 滑钮：位移走弹性曲线 —— 开关是「按下去弹过来」的物理动作 */
.ui-switch__knob {
  transition: transform var(--mc-dur-base) var(--mc-ease-spring);
}

/* 按压：整颗轻微内缩 */
.ui-switch__track:active:not(:disabled) {
  transform: scale(0.96);
}

.ui-switch__track {
  transition:
    background-color 180ms var(--mc-ease-in-out),
    transform 100ms var(--mc-ease-out);
}

@media (prefers-reduced-motion: reduce) {
  .ui-switch__knob {
    transition-duration: 1ms;
  }
}
</style>
