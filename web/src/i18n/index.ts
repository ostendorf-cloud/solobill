import { createI18n } from 'vue-i18n'

import { de, en } from './locales'

/** The only two locales this shell serves. German is the source of truth:
 *  the tax domain this tool works in is German. */
export const SUPPORTED_LOCALES = ['de', 'en'] as const
export type Locale = (typeof SUPPORTED_LOCALES)[number]
export const DEFAULT_LOCALE: Locale = 'de'

export function isLocale(value: string): value is Locale {
  return (SUPPORTED_LOCALES as readonly string[]).includes(value)
}

/** detectPreferredLocale picks English when it is the visitor's highest
 *  weighted browser language, German otherwise. */
export function detectPreferredLocale(languages: readonly string[]): Locale {
  const primary = languages[0]?.split('-')[0]?.toLowerCase()
  return primary === 'en' ? 'en' : DEFAULT_LOCALE
}

/** createAppI18n builds one vue-i18n instance. English falls back to
 *  German for any key not yet translated, rather than showing the raw
 *  key: fallbackLocale is what gives that for free, since German is
 *  authored as the complete set of messages. */
export function createAppI18n(locale: Locale = DEFAULT_LOCALE) {
  return createI18n({
    legacy: false,
    locale,
    fallbackLocale: DEFAULT_LOCALE,
    missingWarn: true,
    fallbackWarn: false,
    messages: { de, en },
  })
}
