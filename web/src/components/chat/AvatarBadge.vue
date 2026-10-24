<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    name: string
    color?: string
    size?: number
    online?: boolean
  }>(),
  { color: '#0052d9', size: 40, online: false },
)

const initial = computed(() => props.name?.trim()?.charAt(0)?.toUpperCase() || '?')
</script>

<template>
  <div class="mc-avatar" :style="{ width: `${size}px`, height: `${size}px`, background: color, fontSize: `${size * 0.45}px` }">
    {{ initial }}
    <span v-if="online" class="mc-avatar__dot" :style="{ width: `${size * 0.25}px`, height: `${size * 0.25}px` }" />
  </div>
</template>

<style scoped>
.mc-avatar {
  position: relative;
  flex-shrink: 0;
  border-radius: 50%;
  color: #fff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  user-select: none;
}

.mc-avatar__dot {
  position: absolute;
  right: 0;
  bottom: 0;
  border-radius: 50%;
  background: var(--td-success-color, #2ba471);
  border: 2px solid var(--td-bg-color-container, #fff);
}
</style>
