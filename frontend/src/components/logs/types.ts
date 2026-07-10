import type { GetLogsServiceType } from '@/api/models'

// LogFilters holds the current state of the log search form. from/to are ISO
// 8601 strings (UTC) ready to be sent as query parameters.
export interface LogFilters {
  serviceType: GetLogsServiceType
  from: string
  to: string
  severities: string[]
  host: string
  // Message substrings that must all match (AND), filtered Dasha-side.
  includes: string[]
  // Negative message substrings (grep -v), filtered Dasha-side.
  excludes: string[]
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

// Search presets prefill message/severity for common investigations. The
// message filter is a plain case-insensitive substring, so each preset picks
// the most selective stable fragment of the PostgreSQL log message; they are
// PG-log oriented (applying one switches service type to postgresql).
export interface LogPreset {
  id: string
  message: string
  severities: string[]
}

export const LOG_PRESETS: LogPreset[] = [
  // "automatic vacuum of table …" + "automatic analyze of table …"
  { id: 'autovacuum', message: 'automatic', severities: ['LOG'] },
  { id: 'deadlock', message: 'deadlock', severities: ['ERROR'] },
  // "checkpoint starting/complete" + "restartpoint starting/complete" on replicas;
  // LOG-only keeps savepoint errors out.
  { id: 'checkpoint', message: 'point', severities: ['LOG'] },
  { id: 'tempFiles', message: 'temporary file', severities: ['LOG'] },
  // statement/lock timeouts and user cancellations
  { id: 'canceled', message: 'canceling statement', severities: ['ERROR'] },
  // auth/connection failures land at FATAL ("password authentication failed", pg_hba)
  { id: 'connections', message: '', severities: ['FATAL'] },
  // log_min_duration_statement output
  { id: 'slow', message: 'duration:', severities: ['LOG'] },
  { id: 'errors', message: '', severities: ['ERROR', 'FATAL', 'PANIC'] },
]

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
