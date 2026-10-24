<script setup lang="ts">
import { computed, ref } from 'vue'

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

/** 默认头像资源不可用时回退为首字母 + 取色底，避免出现空白圆 */
const imageFailed = ref(false)
</script>

<template>
  <div
    class="mc-avatar"
    :style="{ width: `${size}px`, height: `${size}px`, fontSize: `${size * 0.45}px` }"
  >
    <img
      v-if="!imageFailed"
      class="mc-avatar__img"
      src="/default-avatar.svg"
      :alt="name"
      draggable="false"
      @error="imageFailed = true"
    />
    <span v-else class="mc-avatar__initial" :style="{ background: color }">{{ initial }}</span>

    <span
      v-if="online"
      class="mc-avatar__dot"
      :style="{ width: `${size * 0.25}px`, height: `${size * 0.25}px` }"
    />
  </div>
</template>

<style scoped>
.mc-avatar {
  position: relative;
  flex-shrink: 0;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  user-select: none;
}

.mc-avatar__img {
  width: 100%;
  height: 100%;
  border-radius: 50%;
  object-fit: cover;
  display: block;
  /* 与图标自带的浅蓝圆底一致，避免四角透出 */
  background: #e4efff;
}

/* 图片加载失败时的兜底：首字母 + 用户取色 */
.mc-avatar__initial {
  width: 100%;
  height: 100%;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
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
