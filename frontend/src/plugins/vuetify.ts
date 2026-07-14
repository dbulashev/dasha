import { createVuetify } from 'vuetify'
import 'vuetify/styles'

import '@mdi/font/css/materialdesignicons.css' // Ensure you are using css-loader

import { aliases, mdi } from 'vuetify/iconsets/mdi'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { md3 } from 'vuetify/blueprints'
import { de, en, ru } from 'vuetify/locale'


// detect system preference for dark mode and set defaultTheme accordingly
const prefersDark = typeof window !== 'undefined' && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
const defaultTheme = prefersDark ? 'dark' : 'light'

export default createVuetify({
  theme: {
    defaultTheme,
  },
  // The active locale is driven by the locale store (see main.ts); Vuetify only
  // needs every shipped language registered here.
  locale: {
    locale: 'en',
    fallback: 'en',
    messages: { ru, en, de },
  },
  defaults: {
    VDataTable: {
      density: 'compact',
      multiSort: true,
      itemsPerPage: -1,
      hideDefaultFooter: true,
    },
  },
  components,
  directives,
  blueprint: md3,
  icons: {
    defaultSet: 'mdi',
    aliases,
    sets: {
      mdi,
    },
  },
})
