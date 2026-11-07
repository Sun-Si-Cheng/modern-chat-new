<script setup lang="ts">
import { computed } from 'vue'

/**
 * 内联 SVG 图标集：全部 24×24 视口、1.6 描边、currentColor。
 * 官网不引图标库——只有十来个图标，手写比多打 100KB 的依赖划算。
 */
const PATHS: Record<string, string> = {
  'arrow-right': 'M5 12h14M13 6l6 6-6 6',
  'arrow-up-right': 'M7 17 17 7M9 7h8v8',
  copy: 'M9 9h9a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H9a2 2 0 0 1-2-2v-9a2 2 0 0 1 2-2zM5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1',
  check: 'M4 12.5 9 17.5 20 6.5',
  sun: 'M12 4.5v-2M12 21.5v-2M4.5 12h-2M21.5 12h-2M6.7 6.7 5.3 5.3M18.7 18.7l-1.4-1.4M6.7 17.3l-1.4 1.4M18.7 5.3l-1.4 1.4M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z',
  moon: 'M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5z',
  menu: 'M4 7h16M4 12h16M4 17h16',
  close: 'M6 6l12 12M18 6 6 18',
  search: 'M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14zM20 20l-4-4',
  chat: 'M21 12a8 8 0 0 1-8 8H8l-4 3v-4.5A8 8 0 0 1 4 12a8 8 0 0 1 8-8h1a8 8 0 0 1 8 8z',
  grid: 'M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z',
  puzzle: 'M10 4h4v3a2 2 0 1 0 4 0h2v4h-3a2 2 0 1 0 0 4h3v4h-4v-3a2 2 0 1 0-4 0v3H8v-4H5a2 2 0 1 0 0-4h3V6h2a2 2 0 1 0 0-4z',
  settings: 'M12 9.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5zM12 3.5v2M12 18.5v2M3.5 12h2M18.5 12h2M6 6l1.4 1.4M16.6 16.6 18 18M6 18l1.4-1.4M16.6 7.4 18 6',
  shield: 'M12 3 5 6v5.5c0 4.4 2.9 8.3 7 9.5 4.1-1.2 7-5.1 7-9.5V6l-7-3zM9 12l2 2 4-4',
  terminal: 'M5 5l6 7-6 7M13 19h6',
  server: 'M4 4h16v6H4zM4 14h16v6H4zM7.5 7h.01M7.5 17h.01',
  book: 'M4 5.5A2.5 2.5 0 0 1 6.5 3H19v15H6.5A2.5 2.5 0 0 0 4 20.5zM4 20.5A2.5 2.5 0 0 0 6.5 23H19v-5',
  layers: 'M12 3 3 8l9 5 9-5-9-5zM3 13l9 5 9-5M3 17.5l9 5 9-5',
  image: 'M4 5h16v14H4zM4 16l4.5-4.5 3 3L15 11l5 5M9 9.5h.01',
  mic: 'M12 3a3 3 0 0 1 3 3v5a3 3 0 0 1-6 0V6a3 3 0 0 1 3-3zM5.5 11a6.5 6.5 0 0 0 13 0M12 17.5V21',
  users: 'M16 20v-1.5a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4V20M9.5 10.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7zM17 4a3.5 3.5 0 0 1 0 7M21 20v-1.5a4 4 0 0 0-3-3.9',
  lock: 'M6 10.5h12V21H6zM8.5 10.5V7a3.5 3.5 0 0 1 7 0v3.5',
  bolt: 'M13 2 4 14h7l-1 8 9-12h-7l1-8z',
  globe: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM3.5 12h17M12 3c2.5 2.5 3.8 5.6 3.8 9s-1.3 6.5-3.8 9c-2.5-2.5-3.8-5.6-3.8-9S9.5 5.5 12 3z',
  refresh: 'M20 11a8 8 0 0 0-13.7-4.7L4 8.5M4 4v4.5h4.5M4 13a8 8 0 0 0 13.7 4.7L20 15.5M20 20v-4.5h-4.5',
  plus: 'M12 5v14M5 12h14',
  send: 'M4 12 21 4l-8 17-2-7-7-2z',
  paperclip: 'M20 11.5 12 19.5a5 5 0 0 1-7-7l8-8a3.5 3.5 0 0 1 5 5l-8 8a2 2 0 0 1-3-3l7-7',
  smile: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM8.5 14a4.5 4.5 0 0 0 7 0M9 9.5h.01M15 9.5h.01',
  file: 'M6 3h8l4 4v14H6zM14 3v4h4',
  bell: 'M18 16V11a6 6 0 1 0-12 0v5l-2 3h16l-2-3zM10 22h4',
  cpu: 'M7 7h10v10H7zM4 10h3M4 14h3M17 10h3M17 14h3M10 4v3M14 4v3M10 17v3M14 17v3',
  wand: 'M15 4l1.2 2.8L19 8l-2.8 1.2L15 12l-1.2-2.8L11 8l2.8-1.2zM6 20l6-6M4 14l.8 1.8L7 16.5l-2.2.7L4 19l-.8-1.8L1 16.5l2.2-.7z',
  download: 'M12 4v11M7.5 10.5 12 15l4.5-4.5M5 20h14',
  lifetime: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 7v5l3.5 2',
}

const props = withDefaults(
  defineProps<{
    name: keyof typeof PATHS | string
    size?: number | string
    stroke?: number
  }>(),
  { size: 18, stroke: 1.6 },
)

// 图标名会随状态变化（如亮/暗主题按钮），必须用 computed
const d = computed(() => PATHS[props.name] ?? PATHS['arrow-right'])
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    :stroke-width="stroke"
    stroke="currentColor"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path :d="d" />
  </svg>
</template>
