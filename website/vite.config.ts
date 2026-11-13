import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// 官网：纯静态产物，部署在站点根（https://<domain>/）。
// 路由是 history 模式（见 src/router.ts），必须由服务器 try_files 回退到 index.html。
// base 必须是 '/' 而不是 './'：文档页用 fetch('content/docs/*.md') 取正文，
// 相对 base 在 pushState 之后会解析成 /docs/content/docs/*.md（404，页面一片空白）。
export default defineConfig({
  base: '/',
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
