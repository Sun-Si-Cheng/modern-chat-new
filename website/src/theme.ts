/**
 * 主题切换：与主站（web/）共用 localStorage 键 `mc_theme` 与 `theme-mode` 属性，
 * 因此在应用里选过暗色的用户，打开官网也是暗色。
 */
export type ThemeMode = 'light' | 'dark' | 'auto'

const THEME_KEY = 'mc_theme'

function systemDark(): boolean {
  return typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: dark)').matches
}

export function readMode(): ThemeMode {
  try {
    const saved = localStorage.getItem(THEME_KEY)
    if (saved === 'light' || saved === 'dark' || saved === 'auto') return saved
  } catch {
    /* 隐私模式：忽略 */
  }
  return 'auto'
}

export function isDark(mode: ThemeMode = readMode()): boolean {
  return mode === 'dark' || (mode === 'auto' && systemDark())
}

const EVENT = 'mc-theme-change'

/** 应用到 <html theme-mode>；同时广播事件，让内嵌的产品预览跟随站点亮暗 */
export function applyTheme(mode: ThemeMode = readMode()) {
  const dark = isDark(mode)
  document.documentElement.setAttribute('theme-mode', dark ? 'dark' : 'light')
  window.dispatchEvent(new CustomEvent<boolean>(EVENT, { detail: dark }))
}

/** 订阅主题变化（返回取消订阅函数） */
export function onThemeChange(fn: (dark: boolean) => void) {
  const handler = (e: Event) => fn((e as CustomEvent<boolean>).detail)
  window.addEventListener(EVENT, handler)
  return () => window.removeEventListener(EVENT, handler)
}

export function saveMode(mode: ThemeMode) {
  try {
    localStorage.setItem(THEME_KEY, mode)
  } catch {
    /* 隐私模式：忽略 */
  }
  applyTheme(mode)
}

/** 在亮 / 暗之间切换（保留 auto 的语义：从 auto 切到与当前相反的一侧） */
export function toggleTheme(): ThemeMode {
  const next: ThemeMode = isDark() ? 'light' : 'dark'
  saveMode(next)
  return next
}
