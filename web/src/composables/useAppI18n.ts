import { useI18n } from 'vue-i18n'

import { translateError } from '@/i18n/errors'
import { usePrefsStore } from '@/stores/prefs'

/** 统一入口：t 取文案，te 把服务端中文错误转成当前语言。 */
export function useAppI18n() {
  const { t } = useI18n()
  const prefs = usePrefsStore()

  const te = (e: unknown) => {
    const msg = e instanceof Error ? e.message : String(e ?? '')
    return translateError(msg, prefs.lang)
  }

  return { t, te }
}
