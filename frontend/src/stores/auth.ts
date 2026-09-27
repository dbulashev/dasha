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
const LOGIN_REDIRECT_AT_KEY = 'dasha_login_redirect_at'
const LOGIN_LOOP_WINDOW_MS = 15_000

// sessionStorage throws when site data is blocked or the quota is exhausted.
function readSession(key: string): string | null {
  try {
    return sessionStorage.getItem(key)
  } catch {
    return null
  }
}

function writeSession(key: string, value: string | null) {
  try {
    if (value === null) sessionStorage.removeItem(key)
    else sessionStorage.setItem(key, value)
  } catch {
    // The login redirect goes ahead without a saved return address.
  }
}

// `/\` is checked separately: browsers normalise it to `//`, a foreign host.
export function isSafeReturnUrl(url: string): boolean {
  if (!url.startsWith('/') || url.startsWith('//') || url.startsWith('/\\')) return false
  try {
    const parsed = new URL(url, window.location.origin)
    return parsed.origin === window.location.origin && !parsed.pathname.startsWith('/auth/')
  } catch {
    return false
  }
}

export const useAuthStore = defineStore('auth', () => {
  const mode = ref<string>(AuthInfoMode.none)
  const oidcLoginUrl = ref<string | null>(null)
  const user = ref<UserInfo | null>(null)
  const initialized = ref(false)
  const enableQueryStatsReset = ref(false)
  const patEnabled = ref(false)
  const patMinRole = ref('admin')
  let redirecting = false

  const isAuthenticated = computed(() => mode.value === AuthInfoMode.none || user.value !== null)
  const requiresLogin = computed(() => mode.value !== AuthInfoMode.none && !user.value)

  // Admin-equivalent access: the no-RBAC modes (none/token) grant the single
  // operator full access; under OIDC only the mapped admin role does. Used to
  // gate admin-only actions.
  const isAdmin = computed(
    () =>
      mode.value === AuthInfoMode.none ||
      mode.value === AuthInfoMode.token ||
      user.value?.role === 'admin',
  )

  // Whether the signed-in user may manage personal access tokens: the feature
  // must be available (pat_enabled) and the user's role must clear
  // auth.pat_min_role. Combined client-side because /api/auth/info answers
  // before login and carries no caller identity; the server enforces the same
  // rule on token creation.
  const canManageTokens = computed(
    () => patEnabled.value && (patMinRole.value === 'viewer' || user.value?.role === 'admin'),
  )

  // Whether the user may administer other people's tokens and see the user list.
  // Narrower than isAdmin: the server serves these endpoints only to an
  // interactive OIDC admin (a static/personal token is refused), and the user
  // list is only populated by SSO sign-ins. patEnabled also tells us the
  // api_tokens table exists, so the endpoints will not 500.
  const canAdminTokens = computed(
    () => patEnabled.value && mode.value === AuthInfoMode.oidc && user.value?.role === 'admin',
  )

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
      patEnabled.value = res.data.pat_enabled ?? false
      patMinRole.value = res.data.pat_min_role ?? 'admin'
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

  function saveReturnUrl() {
    writeSession(RETURN_URL_KEY, window.location.pathname + window.location.search)
    writeSession(LOGIN_REDIRECT_AT_KEY, String(Date.now()))
  }

  function doLoginRedirect() {
    if (oidcLoginUrl.value) {
      saveReturnUrl()
      window.location.href = oidcLoginUrl.value
    }
  }

  function handleUnauthorized() {
    if (mode.value !== AuthInfoMode.oidc || !oidcLoginUrl.value || redirecting) return

    const lastRedirectAt = Number(readSession(LOGIN_REDIRECT_AT_KEY))
    const elapsed = Date.now() - lastRedirectAt
    if (lastRedirectAt && elapsed >= 0 && elapsed < LOGIN_LOOP_WINDOW_MS) {
      user.value = null
      return
    }

    redirecting = true
    user.value = null
    saveReturnUrl()
    window.location.href = oidcLoginUrl.value
  }

  function consumeReturnUrl(): string | null {
    const url = readSession(RETURN_URL_KEY)
    writeSession(RETURN_URL_KEY, null)
    return url && isSafeReturnUrl(url) ? url : null
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

  return { mode, oidcLoginUrl, user, initialized, isAuthenticated, requiresLogin, isAdmin, enableQueryStatsReset, patEnabled, patMinRole, canManageTokens, canAdminTokens, init, doLoginRedirect, handleUnauthorized, consumeReturnUrl, logout }
})
