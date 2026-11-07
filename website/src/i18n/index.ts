import { createI18n } from 'vue-i18n'

import messages from './messages.clean.json'

export type Lang = 'zh' | 'en'

const LANG_KEY = 'mc_site_lang'

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

/**
 * 开发期自检：vue-i18n 把 `@` 当 linked message、`|` 当 plural、`{}` 当插值，
 * 文案里出现裸 `@` 会在渲染时抛 SyntaxError，整页直接白屏且报错信息晦涩。
 * 这里在 dev 下把所有消息编译一遍，问题在控制台立刻可见。
 */
if (import.meta.env.DEV) {
  const g = i18n.global as unknown as {
    t: (key: string, named: Record<string, unknown>, opts: { locale: string }) => string
  }
  for (const lang of ['zh', 'en'] as Lang[]) {
    for (const key of Object.keys((messages as Record<Lang, Record<string, string>>)[lang])) {
      try {
        g.t(key, {}, { locale: lang })
      } catch (err) {
        console.error(`[i18n] 消息编译失败 (${lang}/${key})`, err)
      }
    }
  }
}

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
