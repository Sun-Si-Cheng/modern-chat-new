<script setup lang="ts">
import { computed } from 'vue'

/** 滑块（原生 range + 定制轨道，替代 TDesign Slider） */
const props = withDefaults(
  defineProps<{
    modelValue?: number
    min?: number
    max?: number
    step?: number
    disabled?: boolean
  }>(),
  { modelValue: 0, min: 0, max: 100, step: 1, disabled: false },
)

const emit = defineEmits<{
  'update:modelValue': [value: number]
  change: [value: number]
}>()

const percent = computed(() => {
  const span = props.max - props.min
  if (span <= 0) return 0
  return Math.min(100, Math.max(0, ((props.modelValue - props.min) / span) * 100))
})

function onInput(e: Event) {
  const v = Number((e.target as HTMLInputElement).value)
  emit('update:modelValue', v)
  emit('change', v)
}
</script>

<template>
  <input
    type="range"
    class="ui-slider"
    :min="min"
    :max="max"
    :step="step"
    :value="modelValue"
    :disabled="disabled"
    :style="{ '--ui-slider-percent': `${percent}%` }"
    @input="onInput"
  />
</template>

<style scoped>
.ui-slider {
  -webkit-appearance: none;
  appearance: none;
  width: 100%;
  height: 18px;
  background: transparent;
  cursor: pointer;
}

.ui-slider:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

/* 已选区间用品牌色，未选为细描边色 */
.ui-slider::-webkit-slider-runnable-track {
  height: 4px;
  border-radius: 2px;
  background: linear-gradient(
    to right,
    var(--td-brand-color) 0%,
    var(--td-brand-color) var(--ui-slider-percent),
    var(--td-component-border, rgba(127,127,127,.35)) var(--ui-slider-percent),
    var(--td-component-border, rgba(127,127,127,.35)) 100%
  );
}

.ui-slider::-webkit-slider-thumb {
  -webkit-appearance: none;
  appearance: none;
  width: 14px;
  height: 14px;
  margin-top: -5px;
  border-radius: 50%;
  background: var(--td-bg-color-container);
  border: 2px solid var(--td-brand-color);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.24);
  transition: transform 120ms ease;
}

.ui-slider:hover::-webkit-slider-thumb {
  transform: scale(1.1);
}

.ui-slider:focus-visible {
  outline: none;
}

.ui-slider:focus-visible::-webkit-slider-thumb {
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--td-brand-color) 30%, transparent);
}

/* Firefox */
.ui-slider::-moz-range-track {
  height: 4px;
  border-radius: 2px;
  background: var(--td-component-border, rgba(127,127,127,.35));
}

.ui-slider::-moz-range-progress {
  height: 4px;
  border-radius: 2px;
  background: var(--td-brand-color);
}

.ui-slider::-moz-range-thumb {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: var(--td-bg-color-container);
  border: 2px solid var(--td-brand-color);
}
</style>
