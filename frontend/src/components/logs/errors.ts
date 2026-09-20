import { ApiError } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

type TFunc = (key: string) => string

export function logErrorText(err: unknown, t: TFunc): string {
  if (err instanceof ApiError) {
    switch (err.status) {
      case 400:
        return t('logs.error.badRequest')
      case 404:
        return t('logs.error.notFound')
      case 501:
        return t('logs.error.unsupported')
      case 502:
        return t('logs.error.upstream')
      case 504:
        return t('logs.error.timeout')
    }
  }
  // A bare fetch rejection (not an ApiError) means no HTTP response arrived —
  // the connection dropped or the request outran the browser/proxy while the
  // backend was still waiting on the log store.
  const msg = getErrorMessage(err)
  if (/failed to fetch|networkerror|load failed|network request failed/i.test(msg)) {
    return t('logs.error.network')
  }
  return msg
}
