import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// 官网：纯静态产物，可托管在任意静态服务器 / 子路径。
// 路由用 hash 模式（见 src/router.ts），因此 base 用相对路径即可，无需服务器 rewrite。
export default defineConfig({
  base: './',
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    chunkSizeWarningLimit: 900,
  },
  server: {
    port: 5174,
  },
})
