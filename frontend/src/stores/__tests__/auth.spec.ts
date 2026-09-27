import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('@/api/gen/default/default', () => ({
  getAuthInfo: vi.fn(),
}))

import { AuthInfoMode } from '@/api/models'
import { isSafeReturnUrl, useAuthStore } from '@/stores/auth'

const RETURN_URL_KEY = 'dasha_return_url'
const LOGIN_URL = '/auth/login'

let hrefs: string[]

function stubLocation(pathname: string, search = '') {
  vi.stubGlobal('location', {
    origin: 'http://localhost',
    pathname,
    search,
    get href() {
      return `http://localhost${pathname}${search}`
    },
    set href(v: string) {
      hrefs.push(v)
    },
  })
}

function oidcStore(mode: string = AuthInfoMode.oidc) {
  setActivePinia(createPinia())
  const auth = useAuthStore()
  auth.mode = mode
  auth.oidcLoginUrl = LOGIN_URL
  auth.user = { name: 'u', email: 'u@example.com', role: 'viewer' }
  return auth
}

beforeEach(() => {
  hrefs = []
  sessionStorage.clear()
  stubLocation('/queries/c1', '?host=h&db=d')
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('handleUnauthorized', () => {
  it('saves the current address and redirects to the IdP', () => {
    const auth = oidcStore()
    auth.handleUnauthorized()

    expect(sessionStorage.getItem(RETURN_URL_KEY)).toBe('/queries/c1?host=h&db=d')
    expect(hrefs).toEqual([LOGIN_URL])
    expect(auth.user).toBeNull()
  })

  it('redirects once for parallel 401s', () => {
    const auth = oidcStore()
    auth.handleUnauthorized()
    auth.handleUnauthorized()
    auth.handleUnauthorized()

    expect(hrefs).toEqual([LOGIN_URL])
  })

  it.each([AuthInfoMode.none, AuthInfoMode.token])('does nothing in %s mode', (mode) => {
    const auth = oidcStore(mode)
    auth.handleUnauthorized()

    expect(sessionStorage.getItem(RETURN_URL_KEY)).toBeNull()
    expect(hrefs).toEqual([])
  })

  it('stops the redirect loop within the window', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'))
    oidcStore().handleUnauthorized()

    vi.setSystemTime(new Date('2026-01-01T00:00:05Z'))
    const auth = oidcStore()
    auth.handleUnauthorized()

    expect(hrefs).toEqual([LOGIN_URL])
    expect(auth.user).toBeNull()
    expect(auth.requiresLogin).toBe(true)
  })

  it('redirects again after the window', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'))
    oidcStore().handleUnauthorized()

    vi.setSystemTime(new Date('2026-01-01T00:00:16Z'))
    oidcStore().handleUnauthorized()

    expect(hrefs).toEqual([LOGIN_URL, LOGIN_URL])
  })

  it('ignores a redirect timestamp from the future', () => {
    sessionStorage.setItem('dasha_login_redirect_at', String(Date.now() + 60_000))
    oidcStore().handleUnauthorized()

    expect(hrefs).toEqual([LOGIN_URL])
  })

  it('redirects when sessionStorage throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('full', 'QuotaExceededError')
    })
    oidcStore().handleUnauthorized()

    expect(hrefs).toEqual([LOGIN_URL])
  })
})

describe('doLoginRedirect', () => {
  it('overwrites a stale return address with the current one', () => {
    sessionStorage.setItem(RETURN_URL_KEY, '/indexes/old')
    stubLocation('/logs/c1', '?q=error')
    oidcStore().doLoginRedirect()

    expect(sessionStorage.getItem(RETURN_URL_KEY)).toBe('/logs/c1?q=error')
    expect(hrefs).toEqual([LOGIN_URL])
  })
})

describe('consumeReturnUrl', () => {
  it('returns the address once', () => {
    sessionStorage.setItem(RETURN_URL_KEY, '/queries/c1?host=h&db=d')
    const auth = oidcStore()

    expect(auth.consumeReturnUrl()).toBe('/queries/c1?host=h&db=d')
    expect(auth.consumeReturnUrl()).toBeNull()
  })

  it('keeps log search filters intact', () => {
    const url = '/logs/c1?q=error&from=2026-01-01T00%3A00%3A00Z'
    sessionStorage.setItem(RETURN_URL_KEY, url)

    expect(oidcStore().consumeReturnUrl()).toBe(url)
  })

  it.each(['//evil.example', 'https://evil.example', '/\\evil.example', '/auth/callback', 'evil'])(
    'drops %s and clears the key',
    (url) => {
      sessionStorage.setItem(RETURN_URL_KEY, url)

      expect(oidcStore().consumeReturnUrl()).toBeNull()
      expect(sessionStorage.getItem(RETURN_URL_KEY)).toBeNull()
    },
  )
})

describe('isSafeReturnUrl', () => {
  it('accepts a same-origin path', () => {
    expect(isSafeReturnUrl('/main/c1')).toBe(true)
  })
})
