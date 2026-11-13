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
// 部署在 /app/ 下：注册路径与 scope 都要带 base（scope 默认取脚本所在目录，天然是 /app/），
// 不注册根路径的 SW，避免影响站点根路径的官网。
//
// 迁移清理（一次性）：旧版本把 SW 注册在站点根（scope '/'）。迁到 /app/ 后
// 根路径的 /sw.js 已不存在，浏览器拿不到更新，那个旧 SW 会一直控制 / 与 /app/：
// 断网时它会把缓存的旧聊天首页当成官网返回。所以这里主动注销所有 scope 不在
// 当前 base 下的注册——只影响本域，不会动官网自己的任何东西。
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  navigator.serviceWorker
    .getRegistrations()
    .then((regs) => {
      regs.forEach((reg) => {
        const scope = new URL(reg.scope).pathname
        if (!scope.startsWith(import.meta.env.BASE_URL)) void reg.unregister()
      })
    })
    .catch(() => {
      /* 取不到注册列表不影响后续注册 */
    })
}

if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register(`${import.meta.env.BASE_URL}sw.js`, { scope: import.meta.env.BASE_URL })
      .catch(() => {
        /* 注册失败不影响正常使用 */
      })
  })
}
