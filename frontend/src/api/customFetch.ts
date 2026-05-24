/**
 * Custom fetch wrapper for orval-generated API clients.
 *
 * Replaces the default inlined `JSON.parse(body)` so that non-JSON responses
 * (e.g. an HTML error page from nginx when the backend is down) do not crash
 * the call with `SyntaxError: Unexpected token '<'`. Instead we synthesize a
 * minimal `{ message }` payload and let `assertOk` produce a normal `ApiError`
 * with the real HTTP status.
 */
export const customFetch = async <T>(url: string, options?: RequestInit): Promise<T> => {
  const res = await fetch(url, options)

  const body = [204, 205, 304].includes(res.status) ? null : await res.text()

  let data: unknown = {}
  if (body) {
    const contentType = res.headers.get('content-type') || ''
    if (contentType.includes('application/json')) {
      try {
        data = JSON.parse(body)
      } catch {
        data = { message: `Invalid JSON response (HTTP ${res.status})` }
      }
    } else {
      data = { message: `HTTP ${res.status}` }
    }
  }

  return { data, status: res.status, headers: res.headers } as T
}
