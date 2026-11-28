import type { FeatureUnavailableReason } from '@/api/models/index'

export class ApiError extends Error {
  status: number
  data: unknown

  constructor(status: number, message: string, data?: unknown) {
    super(message)
    this.status = status
    this.data = data
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
    throw new ApiError(response.status, msg || `HTTP ${response.status}`, response.data)
  }
  return response.data as T
}

const FEATURE_REASONS: readonly string[] = ['unsupported_version', 'no_storage', 'no_privilege']

// The reason of a 501 FeatureUnavailable, or null for any other error.
export function featureUnavailableReason(
  err: unknown,
  fallback: FeatureUnavailableReason,
): FeatureUnavailableReason | null {
  if (!(err instanceof ApiError) || err.status !== 501) return null
  const reason = (err.data as { reason?: unknown } | null)?.reason
  return typeof reason === 'string' && FEATURE_REASONS.includes(reason)
    ? (reason as FeatureUnavailableReason)
    : fallback
}
