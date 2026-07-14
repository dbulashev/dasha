import { defineStore } from 'pinia'
import { ref } from 'vue'

export type AppLocale = 'ru_RU' | 'de_DE' | 'en_US'
export type LocaleSetting = AppLocale | 'auto'

// The locales that ship with the app. `vuetify` is the matching locale for
// Vuetify's own strings (pagination, data tables), which are keyed by language.
export const LOCALES: { value: AppLocale; label: string; vuetify: string }[] = [
  { value: 'ru_RU', label: 'Русский', vuetify: 'ru' },
  { value: 'en_US', label: 'English', vuetify: 'en' },
  { value: 'de_DE', label: 'Deutsch', vuetify: 'de' },
]

export const FALLBACK_LOCALE: AppLocale = 'en_US'

// detectLocale maps the browser's preferred languages onto a shipped locale,
// matching on the language subtag so that ru-RU, ru-BY and ru all resolve to
// ru_RU. Falls back to English when the browser asks for a language we have no
// translation for.
export function detectLocale(): AppLocale {
  const preferred = typeof navigator === 'undefined' ? [] : (navigator.languages ?? [navigator.language])

  for (const lang of preferred) {
    const subtag = lang?.split('-')[0]?.toLowerCase()
    const hit = LOCALES.find((l) => l.value.split('_')[0].toLowerCase() === subtag)
    if (hit) return hit.value
  }

  return FALLBACK_LOCALE
}

export const useLocaleStore = defineStore(
  'locale',
  () => {
    // 'auto' means "not set" — resolved from the browser on every read, so a user
    // who never picks a language follows their browser if it later changes.
    const locale = ref<LocaleSetting>('auto')

    function currentLocale(): AppLocale {
      return locale.value === 'auto' ? detectLocale() : locale.value
    }

    function setLocale(value: LocaleSetting) {
      locale.value = value
    }

    function vuetifyLocale(): string {
      const current = currentLocale()
      return LOCALES.find((l) => l.value === current)?.vuetify ?? 'en'
    }

    return { locale, currentLocale, setLocale, vuetifyLocale }
  },
  {
    persist: {
      storage: localStorage,
    },
  },
)
