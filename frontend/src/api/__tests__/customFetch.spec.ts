import { afterEach, describe, expect, it, vi } from 'vitest'

const handleUnauthorized = vi.fn()

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ handleUnauthorized }),
}))

import { customFetch } from '@/api/customFetch'

function stubFetch(status: number) {
  const body = JSON.stringify({ message: 'm' })
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(
      new Response(body, { status, headers: { 'content-type': 'application/json' } }),
    ),
  )
}

afterEach(() => {
  document.querySelector('base')?.remove()
  vi.unstubAllGlobals()
  handleUnauthorized.mockReset()
})

describe('customFetch', () => {
  it('добавляет runtime-префикс к сгенерированному API URL', async () => {
    document.head.insertAdjacentHTML('afterbegin', '<base href="/dasha/">')
    stubFetch(200)
    await customFetch('/api/clusters?q=a%2Fb')
    expect(fetch).toHaveBeenCalledWith('/dasha/api/clusters?q=a%2Fb', undefined)
  })
  it('reports 401 to the auth store and still returns the response', async () => {
    stubFetch(401)
    const res = await customFetch<{ status: number }>('/api/v1/x')

    expect(handleUnauthorized).toHaveBeenCalledTimes(1)
    expect(res.status).toBe(401)
  })

  it.each([200, 403, 500])('ignores %i', async (status) => {
    stubFetch(status)
    await customFetch('/api/v1/x')

    expect(handleUnauthorized).not.toHaveBeenCalled()
  })
})
