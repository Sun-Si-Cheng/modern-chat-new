import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { currentLang, setLang as applyLang, type Lang } from '@/i18n'

export type Theme = 'auto' | 'light' | 'dark'

/** 认证页背景特效种类 */
export type SceneEffect = 'aurora' | 'waves' | 'grid' | 'none'

/** 全站页面背景纹理 */
export type AppBgPattern = 'none' | 'grid' | 'dots' | 'diagonal'

const THEME_KEY = 'mc_theme'
const SCENE_COLOR_KEY = 'mc_scene_color'
const SCENE_EFFECT_KEY = 'mc_scene_effect'
const APP_BG_COLOR_KEY = 'mc_app_bg_color'
const APP_BG_PATTERN_KEY = 'mc_app_bg_pattern'
const APP_BG_IMAGE_KEY = 'mc_app_bg_image'

export const DEFAULT_SCENE_COLOR = '#123a5c'

/** 全站背景预设色：中低饱和，叠加到主题底色后仍是柔和浅色 */
export const APP_BG_PRESETS = [
  '#6ea8ff',
  '#5fd0b0',
  '#f0a35e',
  '#b28bff',
  '#f07f9a',
  '#8ca3b8',
] as const

/** 全站背景色的不透明度：保证正文/次要文字在亮暗主题下都保持可读 */
const APP_BG_ALPHA = 0.3

/** 背景图 data URL 上限：压缩后仍超过该长度就拒绝，避免撑爆 localStorage */
export const MAX_APP_BG_IMAGE_LEN = 1_500_000

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

/** 全站背景色：空串表示跟随主题（不额外叠色） */
function detectAppBgColor(): string {
  const saved = localStorage.getItem(APP_BG_COLOR_KEY)
  return saved && /^#[0-9a-fA-F]{6}$/.test(saved) ? saved : ''
}

function detectAppBgPattern(): AppBgPattern {
  const saved = localStorage.getItem(APP_BG_PATTERN_KEY)
  if (saved === 'grid' || saved === 'dots' || saved === 'diagonal' || saved === 'none') return saved
  return 'none'
}

/** 背景图：只接受压缩后的 data URL，超限视为未设置 */
function detectAppBgImage(): string {
  const saved = localStorage.getItem(APP_BG_IMAGE_KEY)
  if (!saved || !saved.startsWith('data:image/')) return ''
  return saved.length <= MAX_APP_BG_IMAGE_LEN ? saved : ''
}

/** #rrggbb → rgba()，避免依赖 color-mix 的浏览器兼容性 */
function hexToRgba(hex: string, alpha: number): string {
  const v = hex.replace('#', '')
  const r = parseInt(v.slice(0, 2), 16)
  const g = parseInt(v.slice(2, 4), 16)
  const b = parseInt(v.slice(4, 6), 16)
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

/**
 * 外观偏好：语言 + 主题（跟随系统/亮/暗）+ 认证页背景（颜色与特效）
 * + 全站页面背景（颜色与纹理）。
 * 背景偏好保存在本地，仅影响当前浏览器，不影响其他访客。
 */
export const usePrefsStore = defineStore('prefs', () => {
  const lang = ref<Lang>(currentLang())
  const theme = ref<Theme>(detectTheme())
  const systemDark = ref(window.matchMedia('(prefers-color-scheme: dark)').matches)
  const sceneColor = ref<string>(detectSceneColor())
  const sceneEffect = ref<SceneEffect>(detectSceneEffect())
  const appBgColor = ref<string>(detectAppBgColor())
  const appBgPattern = ref<AppBgPattern>(detectAppBgPattern())
  const appBgImage = ref<string>(detectAppBgImage())

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

  /**
   * 把「纹理 + 颜色 + 图片」拼成一组 background-image 及其配套的
   * size / repeat / position / attachment。层序从上到下：纹理 → 颜色 → 图片柔化层 → 图片。
   *
   * 注意 body 盒高只有视口高（html/body/#app height:100%），设置页这类整页滚动的
   * 页面里文档远高于 body 盒：
   * - 纯色/柔化层必须 repeat（平铺纯色渐变无缝），no-repeat 只刷第一屏 →「上半段变色下半段不变」
   * - 图片层用 fixed 视口锁定（cover 相对视口取尺寸，滚动时也不再露底）
   */
  function buildBgStack(patternVar: string, scrimVar: string) {
    const layers: string[] = []
    const sizes: string[] = []
    const repeats: string[] = []
    const positions: string[] = []
    const attachments: string[] = []
    const push = (
      layer: string,
      size: string,
      repeat: string,
      position = 'center',
      attachment = 'scroll',
    ) => {
      layers.push(layer)
      sizes.push(size)
      repeats.push(repeat)
      positions.push(position)
      attachments.push(attachment)
    }

    if (appBgPattern.value === 'grid') {
      push(`linear-gradient(${patternVar} 1px, transparent 1px)`, '24px 24px', 'repeat')
      push(`linear-gradient(90deg, ${patternVar} 1px, transparent 1px)`, '24px 24px', 'repeat')
    } else if (appBgPattern.value === 'dots') {
      push(`radial-gradient(${patternVar} 1.2px, transparent 1.2px)`, '18px 18px', 'repeat')
    } else if (appBgPattern.value === 'diagonal') {
      push(
        `repeating-linear-gradient(45deg, ${patternVar} 0 1px, transparent 1px 14px)`,
        'auto',
        'repeat',
      )
    }

    if (appBgColor.value) {
      const tint = hexToRgba(appBgColor.value, APP_BG_ALPHA)
      push(`linear-gradient(${tint}, ${tint})`, 'auto', 'repeat')
    }

    if (appBgImage.value) {
      push(`linear-gradient(${scrimVar}, ${scrimVar})`, 'auto', 'repeat', 'center', 'fixed')
      push(`url("${appBgImage.value}")`, 'cover', 'no-repeat', 'center', 'fixed')
    }

    if (!layers.length) {
      return { stack: 'none', size: 'auto', repeat: 'repeat', position: 'center', attachment: 'scroll' }
    }
    return {
      stack: layers.join(', '),
      size: sizes.join(', '),
      repeat: repeats.join(', '),
      position: positions.join(', '),
      attachment: attachments.join(', '),
    }
  }

  /** 全站背景：普通页面与认证场景各生成一套变量 */
  function applyAppBg() {
    const root = document.documentElement
    const page = buildBgStack('var(--mc-app-pattern)', 'var(--mc-app-image-scrim)')
    const scene = buildBgStack('var(--mc-app-pattern-scene)', 'var(--mc-auth-image-scrim)')

    root.style.setProperty('--mc-app-bg-stack', page.stack)
    root.style.setProperty('--mc-app-bg-size', page.size)
    root.style.setProperty('--mc-app-bg-repeat', page.repeat)
    root.style.setProperty('--mc-app-bg-position', page.position)
    root.style.setProperty('--mc-app-bg-attachment', page.attachment)
    root.style.setProperty('--mc-scene-bg-stack', scene.stack)
    root.style.setProperty('--mc-scene-bg-size', scene.size)
    root.style.setProperty('--mc-scene-bg-repeat', scene.repeat)
    root.style.setProperty('--mc-scene-bg-position', scene.position)
    root.style.setProperty('--mc-scene-bg-attachment', scene.attachment)
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

  /** 全站背景色；传空串表示恢复「跟随主题」 */
  function setAppBgColor(color: string) {
    if (color && !/^#[0-9a-fA-F]{6}$/.test(color)) return
    appBgColor.value = color
    if (color) localStorage.setItem(APP_BG_COLOR_KEY, color)
    else localStorage.removeItem(APP_BG_COLOR_KEY)
    applyAppBg()
  }

  function setAppBgPattern(pattern: AppBgPattern) {
    appBgPattern.value = pattern
    localStorage.setItem(APP_BG_PATTERN_KEY, pattern)
    applyAppBg()
  }

  /** 设置背景图（压缩后的 data URL）；传空串清除。返回是否写入成功 */
  function setAppBgImage(image: string): boolean {
    if (image && (!image.startsWith('data:image/') || image.length > MAX_APP_BG_IMAGE_LEN)) {
      return false
    }
    appBgImage.value = image
    try {
      if (image) localStorage.setItem(APP_BG_IMAGE_KEY, image)
      else localStorage.removeItem(APP_BG_IMAGE_KEY)
    } catch {
      // 超出浏览器配额：回退到未设置，避免界面显示一张下次刷新就丢失的图
      appBgImage.value = ''
      localStorage.removeItem(APP_BG_IMAGE_KEY)
      applyAppBg()
      return false
    }
    applyAppBg()
    return true
  }

  function resetAppBg() {
    setAppBgColor('')
    setAppBgPattern('none')
    setAppBgImage('')
  }

  function init() {
    applyTheme()
    applyScene()
    applyAppBg()
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
    appBgColor,
    appBgPattern,
    appBgImage,
    init,
    setLang,
    toggleLang,
    setTheme,
    cycleTheme,
    setSceneColor,
    setSceneEffect,
    resetScene,
    setAppBgColor,
    setAppBgPattern,
    setAppBgImage,
    resetAppBg,
  }
})
