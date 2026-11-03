import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueDevTools from 'vite-plugin-vue-devtools'
import UnoCSS from 'unocss/vite'

// https://vite.dev/config/
export default defineConfig(({ mode }) => ({
  // devtools 仅开发启用：生产构建不应包含它（体积与运行时开销）
  plugins: [vue(), UnoCSS(), ...(mode === 'development' ? [vueDevTools()] : [])],
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
  // preview（生产构建预览）默认不带代理，导致本地跑 preview 时接口全 404，
  // 也就无法测量真实的生产性能。这里与 dev 保持一致。
  preview: {
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
}))
