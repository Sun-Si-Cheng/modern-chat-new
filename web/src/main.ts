import { createApp } from 'vue'
import { createPinia } from 'pinia'
import TDesign from 'tdesign-vue-next'

import App from './App.vue'
import router from './router'
import i18n from './i18n'
import { usePrefsStore } from './stores/prefs'

import 'tdesign-vue-next/es/style/index.css'
import './assets/main.css'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(i18n)
app.use(router)
app.use(TDesign)

// 外观偏好：先应用主题与 <html lang>，避免首屏闪烁
usePrefsStore(pinia).init()

app.mount('#app')
