import { createApp } from 'vue'
import { createPinia } from 'pinia'
import piniaPluginPersistedstate from 'pinia-plugin-persistedstate'

import { createI18n } from 'vue-i18n'

import ruRU from './locales/ru_RU.json'
import createVuetify from './plugins/vuetify'

const pinia = createPinia()

import App from './App.vue'
import router from './router'


const app = createApp(App)

const i18n = createI18n({
    legacy: false,
    globalInjection: true,
    locale: 'ru_RU',
    fallbackLocale: 'en',
    messages: {
        'ru_RU': ruRU
    }
})


pinia.use(piniaPluginPersistedstate)

app.use(pinia)
app.use(createVuetify)

app.use(i18n)
app.use(router)

app.mount('#app')
