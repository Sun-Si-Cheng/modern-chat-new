import { createApp } from 'vue'

import App from './App.vue'
import { i18n, setLang } from './i18n'
import { router } from './router'
import { applyTheme, readMode } from './theme'

// 全局样式：站点基础样式（沿用原静态站视觉）+ TDesign 组件样式
import './styles/site.css'
import 'tdesign-vue-next/es/style/index.css'

const app = createApp(App)

app.use(router)
app.use(i18n)

// 首屏前应用语言（避免 <html lang> 与文案跳变）
setLang(i18n.global.locale.value as 'zh' | 'en')

// 主题：index.html 已做首屏预设，这里补上"跟随系统"的实时响应
applyTheme()
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
  if (readMode() === 'auto') applyTheme()
})

app.mount('#app')
