import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { currentLang, setLang as applyLang, type Lang } from '@/i18n'

export type Theme = 'auto' | 'light' | 'dark'

/** 认证页背景特效种类 */
export type SceneEffect = 'aurora' | 'waves' | 'grid' | 'none'

const THEME_KEY = 'mc_theme'
const SCENE_COLOR_KEY = 'mc_scene_color'
const SCENE_EFFECT_KEY = 'mc_scene_effect'

export const DEFAULT_SCENE_COLOR = '#123a5c'

/** 预设背景色：均为中低明度，白色光晕在其上可见且卡片对比度稳定 */
export const SCENE_PRESETS = [
  '#123a5c',
  '#0d3b40',
  '#2a2450',
  '#3b2a1f',
  '#1d2b23',
  '#241c33',
] as const

function detectTheme(): Theme {
  const saved = localStorage.getItem(THEME_KEY)
  if (saved === 'light' || saved === 'dark' || saved === 'auto') return saved
  return 'auto'
}

function detectSceneEffect(): SceneEffect {
  const saved = localStorage.getItem(SCENE_EFFECT_KEY)
  if (saved === 'aurora' || saved === 'waves' || saved === 'grid' || saved === 'none') return saved
  return 'aurora'
}

function detectSceneColor(): string {
  const saved = localStorage.getItem(SCENE_COLOR_KEY)
  return saved && /^#[0-9a-fA-F]{6}$/.test(saved) ? saved : DEFAULT_SCENE_COLOR
}

/**
 * 外观偏好：语言 + 主题（跟随系统/亮/暗）+ 认证页背景（颜色与特效）。
 * 背景偏好保存在本地，仅影响当前浏览器，不影响其他访客。
 */
export const usePrefsStore = defineStore('prefs', () => {
  const lang = ref<Lang>(currentLang())
  const theme = ref<Theme>(detectTheme())
  const systemDark = ref(window.matchMedia('(prefers-color-scheme: dark)').matches)
  const sceneColor = ref<string>(detectSceneColor())
  const sceneEffect = ref<SceneEffect>(detectSceneEffect())

  const resolvedTheme = computed<'light' | 'dark'>(() =>
    theme.value === 'auto' ? (systemDark.value ? 'dark' : 'light') : theme.value,
  )

  function applyTheme() {
    document.documentElement.setAttribute('theme-mode', resolvedTheme.value)
  }

  /** 背景基色写成全局 CSS 变量，AuthScene 直接消费 */
  function applyScene() {
    document.documentElement.style.setProperty('--mc-scene-color', sceneColor.value)
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

  function setSceneColor(color: string) {
    if (!/^#[0-9a-fA-F]{6}$/.test(color)) return
    sceneColor.value = color
    localStorage.setItem(SCENE_COLOR_KEY, color)
    applyScene()
  }

  function setSceneEffect(effect: SceneEffect) {
    sceneEffect.value = effect
    localStorage.setItem(SCENE_EFFECT_KEY, effect)
  }

  function resetScene() {
    setSceneColor(DEFAULT_SCENE_COLOR)
    setSceneEffect('aurora')
  }

  function init() {
    applyTheme()
    applyScene()
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    mq.addEventListener('change', (e) => {
      systemDark.value = e.matches
      if (theme.value === 'auto') applyTheme()
    })
    document.documentElement.setAttribute('lang', lang.value === 'zh-CN' ? 'zh-CN' : 'en')
  }

  return {
    lang,
    theme,
    resolvedTheme,
    sceneColor,
    sceneEffect,
    init,
    setLang,
    toggleLang,
    setTheme,
    cycleTheme,
    setSceneColor,
    setSceneEffect,
    resetScene,
  }
})
