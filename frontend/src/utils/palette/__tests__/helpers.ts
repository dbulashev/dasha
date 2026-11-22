import { createI18n } from 'vue-i18n'

import enUS from '@/locales/en_US.json'
import ruRU from '@/locales/ru_RU.json'
import type { Translate } from '@/utils/palette/sections'

// The same call the palette makes: t(key, {}, { locale }) for the English names.
export function translator(locale: 'ru_RU' | 'en_US'): Translate {
  const i18n = createI18n({
    legacy: false,
    locale,
    fallbackLocale: 'en_US',
    messages: { ru_RU: ruRU, en_US: enUS },
  })
  const t = i18n.global.t
  return (key, l) => (l ? t(key, {}, { locale: l as typeof locale }) : t(key))
}
