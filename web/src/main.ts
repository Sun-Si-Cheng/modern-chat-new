import { createApp } from 'vue'
import { createPinia } from 'pinia'

import App from './App.vue'
import router from './router'
import i18n from './i18n'
import { usePrefsStore } from './stores/prefs'

// 样式全量引入（体积仅数十 KB，且避免漏样式）；
// 组件一律在 SFC 内按需 import（不 app.use(TDesign)），否则整库无法 tree-shake。
import 'tdesign-vue-next/es/style/index.css'
import './assets/main.css'
// UnoCSS 工具类最后引入：迁移期需要用原子类覆盖存量组件的默认观感
import 'virtual:uno.css'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(i18n)
app.use(router)

// 外观偏好：先应用主题与 <html lang>，避免首屏闪烁
usePrefsStore(pinia).init()

app.mount('#app')

// PWA：生产环境注册 Service Worker（缓存静态资源，断网时保留应用外壳）
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {
      /* 注册失败不影响正常使用 */
    })
  })
}
