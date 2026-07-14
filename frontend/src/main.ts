import { createApp, watch } from 'vue'
import { createPinia } from 'pinia'
import piniaPluginPersistedstate from 'pinia-plugin-persistedstate'

import { createI18n } from 'vue-i18n'

import deDE from './locales/de_DE.json'
import enUS from './locales/en_US.json'
import ruRU from './locales/ru_RU.json'
import vuetify from './plugins/vuetify'
import { FALLBACK_LOCALE, useLocaleStore } from './stores/locale'

const pinia = createPinia()

import App from './App.vue'
import router from './router'


const app = createApp(App)

pinia.use(piniaPluginPersistedstate)
app.use(pinia)

// The persisted locale store is the source of truth, so it has to be read after
// Pinia is installed — hence the store lookup here rather than a hardcoded locale.
const localeStore = useLocaleStore()

const i18n = createI18n({
    legacy: false,
    globalInjection: true,
    locale: localeStore.currentLocale(),
    fallbackLocale: FALLBACK_LOCALE,
    messages: {
        'ru_RU': ruRU,
        'de_DE': deDE,
        'en_US': enUS
    },
    pluralRules: {
        'ru_RU': (choice: number) => {
            const abs = Math.abs(choice)
            const mod10 = abs % 10
            const mod100 = abs % 100
            if (mod10 === 1 && mod100 !== 11) return 0
            if (mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20)) return 1
            return 2
        }
    }
})

// Keep vue-i18n and Vuetify's own strings in step with the store.
watch(
    () => localeStore.locale,
    () => {
        i18n.global.locale.value = localeStore.currentLocale()
        vuetify.locale.current.value = localeStore.vuetifyLocale()
    },
    { immediate: true },
)

app.use(vuetify)

app.use(i18n)
app.use(router)

router.isReady().then(() => {
    app.mount('#app')
})
