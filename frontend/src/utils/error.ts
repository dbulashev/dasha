import { ApiError } from '@/utils/api'

/** Extracts a human-readable message from an unknown caught value. */
export function getErrorMessage(err: unknown): string {
  if (err instanceof Error) return err.message
  if (typeof err === 'string') return err
  return String(err)
}

// The backend answers a stalled database query with its own status code; those
// have an actionable explanation, so the localized text beats the raw server
// message wherever such an error is shown.
const statusMessages: Record<number, string> = {
  423: 'errorObjectLocked',
  504: 'errorQueryTimeout',
}

/** i18n key explaining an HTTP status, when there is one. */
export function statusMessageKey(code: number): string | undefined {
  return statusMessages[code]
}

/** Localized text for a caught API error, falling back to its own message. */
export function apiErrorText(err: unknown, t: (key: string) => string): string {
  const key = err instanceof ApiError ? statusMessageKey(err.status) : undefined
  return key ? t(key) : getErrorMessage(err)
}
