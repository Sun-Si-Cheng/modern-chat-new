import { createApp } from 'vue'

import App from './App.vue'
import { i18n, setLang } from './i18n'
import { router } from './router'
import { applyTheme, readMode } from './theme'

// 样式分层：令牌 → 基础层 → 站点外壳 → 各页面
import './styles/tokens.css'
import './styles/base.css'
import './styles/site.css'
import './styles/home.css'
import './styles/docs.css'
import './styles/market.css'

const app = createApp(App)

app.use(router)
app.use(i18n)

// 首屏前应用语言（避免 <html lang> 与文案跳变）
setLang(i18n.global.locale.value as 'zh' | 'en')

// 主题：index.html 已做首屏预设，这里补上「跟随系统」的实时响应
applyTheme()
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
  if (readMode() === 'auto') applyTheme()
})

app.mount('#app')
