import { useAuthStore } from '@/stores/auth'
import { AuthInfoMode } from '@/api/models'

/**
 * Asserts that the API response has a successful HTTP status (2xx).
 * Orval-generated fetch functions don't throw on HTTP errors —
 * they resolve with { data, status, headers } regardless of status code.
 * This helper throws an Error for non-2xx responses so that
 * existing try/catch/finally blocks work correctly.
 *
 * For 401 responses in OIDC mode, redirects to the login page.
 */
export function assertOk<T>(response: { data: T; status: number } | { data: unknown; status: number }): T {
  if (response.status === 401) {
    handleUnauthorized()
    throw new Error(`API error: HTTP 401 Unauthorized`)
  }
  if (response.status < 200 || response.status >= 300) {
    throw new Error(`API error: HTTP ${response.status}`)
  }
  return response.data as T
}

function handleUnauthorized() {
  try {
    const auth = useAuthStore()
    if (auth.mode === AuthInfoMode.oidc && auth.oidcLoginUrl) {
      auth.user = null
      window.location.href = auth.oidcLoginUrl
    }
  } catch {
    // Store not yet initialized — ignore.
  }
}
