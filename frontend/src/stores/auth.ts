import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { getAuthInfo } from '@/api/gen/default/default'
import { AuthInfoMode } from '@/api/models'

interface UserInfo {
  name: string
  email: string
  role: string
}

const REDIRECT_KEY = 'dasha_auth_redirect_ts'
const REDIRECT_COOLDOWN_MS = 5000

export const useAuthStore = defineStore('auth', () => {
  const mode = ref<string>(AuthInfoMode.none)
  const oidcLoginUrl = ref<string | null>(null)
  const user = ref<UserInfo | null>(null)
  const initialized = ref(false)
  const redirecting = ref(false)

  const isAuthenticated = computed(() => mode.value === AuthInfoMode.none || user.value !== null)
  const requiresLogin = computed(() => mode.value === AuthInfoMode.oidc && !user.value)
  const ready = computed(() => initialized.value && !redirecting.value)

  function canRedirect(): boolean {
    const last = Number(sessionStorage.getItem(REDIRECT_KEY) || '0')
    return Date.now() - last >= REDIRECT_COOLDOWN_MS
  }

  async function init() {
    if (initialized.value) return

    try {
      const res = await getAuthInfo()
      mode.value = res.data.mode
      oidcLoginUrl.value = res.data.oidc_login_url ?? null
    } catch {
      mode.value = AuthInfoMode.none
    }

    if (mode.value === AuthInfoMode.oidc) {
      try {
        const meRes = await fetch('/auth/me')
        if (meRes.ok) {
          user.value = await meRes.json()
          sessionStorage.removeItem(REDIRECT_KEY)
        }
      } catch {
        user.value = null
      }
    }

    if (requiresLogin.value && oidcLoginUrl.value && canRedirect()) {
      redirecting.value = true
      sessionStorage.setItem(REDIRECT_KEY, String(Date.now()))
    }

    initialized.value = true
  }

  function doLoginRedirect() {
    if (oidcLoginUrl.value) {
      window.location.href = oidcLoginUrl.value
    }
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

  return { mode, oidcLoginUrl, user, initialized, redirecting, ready, isAuthenticated, requiresLogin, init, doLoginRedirect, logout }
})
