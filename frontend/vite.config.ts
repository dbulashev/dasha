import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueJsx from '@vitejs/plugin-vue-jsx'
import vueDevTools from 'vite-plugin-vue-devtools'
import ViteFonts from 'unplugin-fonts/vite'

// Backend the dev server proxies to: a demo stand or a remote install instead of
// a local backend.
const apiTarget = process.env.DASHA_API ?? 'http://localhost:8000'

// https://vite.dev/config/
export default defineConfig({
  build: {
    modulePreload: false,
  },
  plugins: [
    vue(),
    vueJsx(),
    vueDevTools(),
    ViteFonts({
      fontsource: {
        families: [
          {
            name: 'Roboto',
            weights: [100, 300, 400, 500, 700, 900],
            styles: ['normal', 'italic'],
          },
        ],
      },
      // unplugin-fonts preloads every font file emitted into the bundle,
      // including the eot/woff/ttf fallbacks of @mdi/font. Browsers only ever
      // download woff2, so those preloads just warn in the console (font/eot
      // is not even a valid preload type). Keep woff2 preloads only.
      custom: {
        families: [],
        linkFilter: (tags) =>
          tags.filter((tag) => tag.attrs?.rel !== 'preload' || tag.attrs?.type === 'font/woff2'),
      },
    }),
  ],
  server: {
    // Dev-server only. Vite 6 rejects any Host header except localhost by
    // default; the OIDC demo issuer is reached via the "keycloak" hostname
    // (127.0.0.1 keycloak in /etc/hosts), so allow it. Does not affect the
    // production build (served by nginx).
    allowedHosts: ['keycloak', ...(process.env.DASHA_DEV_HOSTS?.split(',') ?? [])],
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
        secure: false,
        // A log scan reads its whole window before answering: a day of records
        // takes tens of seconds, and the backend bounds itself anyway.
        timeout: 120000,
      },
      '/auth': {
        target: apiTarget,
        changeOrigin: true,
        secure: false,
        timeout: 10000,
      },
    },
  },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    },
  },
})
