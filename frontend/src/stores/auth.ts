import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { getAuthInfo } from '@/api/gen/default/default'
import { AuthInfoMode } from '@/api/models'

interface UserInfo {
  name: string
  email: string
  role: string
}

const RETURN_URL_KEY = 'dasha_return_url'

export const useAuthStore = defineStore('auth', () => {
  const mode = ref<string>(AuthInfoMode.none)
  const oidcLoginUrl = ref<string | null>(null)
  const user = ref<UserInfo | null>(null)
  const initialized = ref(false)
  const enableQueryStatsReset = ref(false)

  const isAuthenticated = computed(() => mode.value === AuthInfoMode.none || user.value !== null)
  const requiresLogin = computed(() => mode.value !== AuthInfoMode.none && !user.value)

  async function init() {
    if (initialized.value) return

    try {
      const res = await getAuthInfo()
      if (res.status < 200 || res.status >= 300) {
        throw new Error(`HTTP ${res.status}`)
      }
      mode.value = res.data.mode
      oidcLoginUrl.value = res.data.oidc_login_url ?? null
      enableQueryStatsReset.value = res.data.enable_query_stats_reset ?? false
    } catch {
      mode.value = AuthInfoMode.none
    }

    if (mode.value === AuthInfoMode.oidc) {
      try {
        const meRes = await fetch('/auth/me')
        if (meRes.ok) {
          user.value = await meRes.json()
        }
      } catch {
        user.value = null
      }
    }

    initialized.value = true
  }

  function doLoginRedirect() {
    if (oidcLoginUrl.value) {
      sessionStorage.setItem(RETURN_URL_KEY, window.location.pathname + window.location.search)
      window.location.href = oidcLoginUrl.value
    }
  }

  function consumeReturnUrl(): string | null {
    const url = sessionStorage.getItem(RETURN_URL_KEY)
    if (url) {
      sessionStorage.removeItem(RETURN_URL_KEY)
    }
    return url
  }

  async function logout() {
    try {
      const res = await fetch('/auth/logout', { method: 'POST' })

      if (res.ok) {
        const data = await res.json()
        user.value = null

        if (data.logout_url) {
          window.location.href = data.logout_url
          return
        }
      }
    } catch {
      // Ignore errors.
    }

    user.value = null
    window.location.href = '/'
  }

  return { mode, oidcLoginUrl, user, initialized, isAuthenticated, requiresLogin, enableQueryStatsReset, init, doLoginRedirect, consumeReturnUrl, logout }
})
