export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

/**
 * Asserts that the API response has a successful HTTP status (2xx).
 * Orval-generated fetch functions don't throw on HTTP errors —
 * they resolve with { data, status, headers } regardless of status code.
 * This helper throws an ApiError for non-2xx responses so that
 * existing try/catch/finally blocks work correctly.
 */
export function assertOk<T>(response: { data: T; status: number } | { data: unknown; status: number }): T {
  if (response.status === 401) {
    throw new ApiError(401, 'Unauthorized')
  }
  if (response.status < 200 || response.status >= 300) {
    const msg = (response.data as { message?: string })?.message
    throw new ApiError(response.status, msg || `HTTP ${response.status}`)
  }
  return response.data as T
}
