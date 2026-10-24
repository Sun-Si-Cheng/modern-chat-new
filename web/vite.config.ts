import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueDevTools from 'vite-plugin-vue-devtools'

// https://vite.dev/config/
export default defineConfig({
  // devtools 仅开发启用：生产构建不应包含它（体积与运行时开销）
  plugins: [vue(), { ...vueDevTools(), apply: 'serve' }],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        // 只把框架核心固定为 vendor；TDesign 组件跟随引用它的路由分包，
        // 这样首屏（登录页）不会连带下载只有聊天页/后台才用的组件（Drawer/List/Table 等）。
        manualChunks(id: string) {
          if (!id.includes('node_modules')) return
          if (/node_modules\/(vue|vue-router|pinia|vue-i18n|@vue)\//.test(id)) return 'vendor'
        },
      },
    },
  },
})
