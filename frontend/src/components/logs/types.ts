import type { GetLogsServiceType } from '@/api/models'

// LogFilters holds the current state of the log search form. from/to are ISO
// 8601 strings (UTC) ready to be sent as query parameters.
export interface LogFilters {
  serviceType: GetLogsServiceType
  from: string
  to: string
  severities: string[]
  host: string
  message: string
  database: string
  user: string
  dedup: boolean
  pageSize: number
  // Display order of chronological (non-dedup) results: 'desc' = newest first.
  order: LogOrder
}

export type LogOrder = 'asc' | 'desc'

// Severity options per service type — postgresql uses UPPER case, the pooler
// (Odyssey) uses lower case, matching what the Yandex API expects.
export const SEVERITIES_POSTGRESQL = ['DEBUG', 'LOG', 'INFO', 'NOTICE', 'WARNING', 'ERROR', 'FATAL', 'PANIC']
export const SEVERITIES_POOLER = ['debug', 'info', 'warning', 'error', 'fatal']

export function severityOptions(serviceType: GetLogsServiceType): string[] {
  return serviceType === 'pooler' ? SEVERITIES_POOLER : SEVERITIES_POSTGRESQL
}

// severityColor maps a severity (either case) to a Vuetify chip color.
export function severityColor(severity: string | undefined): string {
  switch ((severity ?? '').toLowerCase()) {
    case 'panic':
    case 'fatal':
    case 'error':
      return 'error'
    case 'warning':
      return 'warning'
    case 'notice':
      return 'info'
    default:
      return 'default'
  }
}
