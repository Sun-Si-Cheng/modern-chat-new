import { createI18n } from 'vue-i18n'

import lists from './lists.json'
import messages from './messages.clean.json'

export type Lang = 'zh' | 'en'

const LANG_KEY = 'mc_site_lang'

/** 随机取一条列表文案（演示 / playground 用） */
export function pickList(lang: Lang, key: string): string[] {
  const bucket = (lists as Record<Lang, Record<string, string[]>>)[lang]
  return bucket?.[key] ?? []
}

function detectLang(): Lang {
  try {
    const saved = localStorage.getItem(LANG_KEY)
    if (saved === 'zh' || saved === 'en') return saved
  } catch {
    /* 隐私模式：忽略 */
  }
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

export const i18n = createI18n({
  legacy: false,
  locale: detectLang(),
  fallbackLocale: 'zh',
  messages: messages as Record<string, Record<string, string>>,
})

export function setLang(lang: Lang) {
  i18n.global.locale.value = lang
  try {
    localStorage.setItem(LANG_KEY, lang)
  } catch {
    /* 隐私模式：忽略 */
  }
  document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en'
}

export function currentLang(): Lang {
  return i18n.global.locale.value as Lang
}

export function toggleLang() {
  setLang(currentLang() === 'zh' ? 'en' : 'zh')
}
