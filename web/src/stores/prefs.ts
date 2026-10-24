import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { currentLang, setLang as applyLang, type Lang } from '@/i18n'

export type Theme = 'auto' | 'light' | 'dark'

const THEME_KEY = 'mc_theme'

function detectTheme(): Theme {
  const saved = localStorage.getItem(THEME_KEY)
  if (saved === 'light' || saved === 'dark' || saved === 'auto') return saved
  return 'auto'
}

/**
 * 外观偏好：语言（中/英）+ 主题（跟随系统 / 亮 / 暗）。
 * 主题通过 TDesign 约定的 `:root[theme-mode]` 切换，全部 --td-* 令牌随之生效。
 */
export const usePrefsStore = defineStore('prefs', () => {
  const lang = ref<Lang>(currentLang())
  const theme = ref<Theme>(detectTheme())
  const systemDark = ref(window.matchMedia('(prefers-color-scheme: dark)').matches)

  const resolvedTheme = computed<'light' | 'dark'>(() =>
    theme.value === 'auto' ? (systemDark.value ? 'dark' : 'light') : theme.value,
  )

  function applyTheme() {
    document.documentElement.setAttribute('theme-mode', resolvedTheme.value)
  }

  function setLang(l: Lang) {
    lang.value = l
    applyLang(l)
  }

  function toggleLang() {
    setLang(lang.value === 'zh-CN' ? 'en-US' : 'zh-CN')
  }

  function setTheme(t: Theme) {
    theme.value = t
    localStorage.setItem(THEME_KEY, t)
    applyTheme()
  }

  /** 三态循环：跟随系统 → 亮 → 暗 */
  function cycleTheme() {
    const order: Theme[] = ['auto', 'light', 'dark']
    setTheme(order[(order.indexOf(theme.value) + 1) % order.length]!)
  }

  function init() {
    applyTheme()
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    mq.addEventListener('change', (e) => {
      systemDark.value = e.matches
      if (theme.value === 'auto') applyTheme()
    })
    document.documentElement.setAttribute('lang', lang.value === 'zh-CN' ? 'zh-CN' : 'en')
  }

  return { lang, theme, resolvedTheme, init, setLang, toggleLang, setTheme, cycleTheme }
})
