import { createApp } from 'vue'

import App from './App.vue'
import { i18n, setLang } from './i18n'
import { router } from './router'

// 全局样式：站点基础样式（沿用原静态站视觉）+ TDesign 组件样式
import './styles/site.css'
import 'tdesign-vue-next/es/style/index.css'

const app = createApp(App)

app.use(router)
app.use(i18n)

// 首屏前应用语言（避免 <html lang> 与文案跳变）
setLang(i18n.global.locale.value as 'zh' | 'en')

app.mount('#app')
