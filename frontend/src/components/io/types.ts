import { fmtCompact, fmtCompactFloat, fmtScaled, pickTimeScale } from '@/utils/format'
import { NEUTRAL_SLOT, SERIES_SLOTS, neutralColor, paletteColor } from '@/utils/seriesColors'

export { neutralColor, paletteColor }

export type MetricMode = 'count' | 'time'

// pg_stat_io was added in PostgreSQL 16.
export const IO_MIN_VERSION_NUM = 160000

// pg_stat_io column names in display order; they are the labels too.
export const COUNT_METRICS = [
  'hits',
  'reads',
  'writes',
  'writebacks',
  'extends',
  'evictions',
  'reuses',
  'fsyncs',
] as const

export const TIME_METRICS = [
  'read_time',
  'write_time',
  'writeback_time',
  'extend_time',
  'fsync_time',
] as const

export const BYTE_METRICS = ['read_bytes', 'write_bytes', 'extend_bytes'] as const

// PG 18 adds the WAL rows; they share the view but not the buffer cache.
export const VISIBLE_OBJECTS = ['relation', 'temp relation', 'wal']

export const BUFFER_OBJECTS = ['relation', 'temp relation']

export const CONTEXTS = ['normal', 'vacuum', 'bulkread', 'bulkwrite'] as const

// Colour follows the entity, not its rank: filtering must not repaint a series.
const CONTEXT_SLOTS: Record<string, readonly [string, string]> = {
  normal: SERIES_SLOTS[0],
  vacuum: SERIES_SLOTS[3],
  bulkread: SERIES_SLOTS[4],
  bulkwrite: SERIES_SLOTS[5],
  init: NEUTRAL_SLOT,
}

// Backend types with a hue of their own; anything else folds into one neutral series.
const BACKEND_TYPE_SLOTS: Record<string, number> = {
  'client backend': 0,
  'autovacuum worker': 1,
  // PG 18 moves much of the reading into these.
  'io worker': 2,
  checkpointer: 3,
  'background writer': 4,
  'background worker': 5,
  startup: 6,
  walsender: 7,
}

export function contextColor(context: string, dark: boolean): string {
  return (CONTEXT_SLOTS[context] ?? NEUTRAL_SLOT)[dark ? 1 : 0]
}

export function backendTypeColor(backendType: string, dark: boolean): string | null {
  const slot = BACKEND_TYPE_SLOTS[backendType]
  return slot === undefined ? null : paletteColor(slot, dark)
}

// Sequential ramp on one hue: transparent for idle, saturated for the peak.
export function heatColor(intensity: number, dark: boolean): string {
  const rgb = dark ? '57, 135, 229' : '42, 120, 214'
  const clamped = Math.min(1, Math.max(0, intensity))
  return `rgba(${rgb}, ${(0.06 + 0.34 * clamped).toFixed(3)})`
}

// One cell of the matrix over the selected window.
export interface MatrixRow {
  backend_type: string
  object: string
  context: string
  values: Record<string, number>
}

// Byte counters ride with the counts; the version difference is resolved in the backend.
export function metricsFor(mode: MetricMode): readonly string[] {
  return mode === 'count' ? [...COUNT_METRICS, ...BYTE_METRICS] : TIME_METRICS
}

export function isByteMetric(name: string): boolean {
  return name.endsWith('_bytes')
}

export function sumValues(rows: MatrixRow[], metric: string): number {
  return rows.reduce((acc, r) => acc + (r.values[metric] ?? 0), 0)
}

export function sumBy(
  rows: MatrixRow[],
  key: (r: MatrixRow) => string,
): Map<string, MatrixRow[]> {
  const out = new Map<string, MatrixRow[]>()
  for (const r of rows) {
    const k = key(r)
    const bucket = out.get(k)
    if (bucket) bucket.push(r)
    else out.set(k, [r])
  }
  return out
}

// hits / (hits + reads); null when the context saw no buffer access at all.
export function hitRatio(rows: MatrixRow[]): number | null {
  const hits = sumValues(rows, 'hits')
  const reads = sumValues(rows, 'reads')
  const total = hits + reads
  return total > 0 ? hits / total : null
}

// Unit suffixes for pickTimeScale; English, like the column names beside them.
const TIME_UNITS: Record<string, string> = {
  us: 'µs',
  ms: 'ms',
  sec: 's',
  min: 'min',
  h: 'h',
}

// Rate over the window: operations per second, or milliseconds of I/O per second
// observed. The time rate can exceed 1 s/s — counters are summed across backends.
export function fmtMetricRate(value: number, seconds: number, mode: MetricMode): string {
  if (seconds <= 0) return '—'

  const perSecond = value / seconds
  if (mode === 'count') return `${fmtCompactFloat(perSecond)}/s`

  const scale = pickTimeScale(perSecond)
  return `${fmtScaled(perSecond, scale)} ${TIME_UNITS[scale.unit]}/s`
}

// A wide window puts I/O totals in the hours; the unit follows the magnitude.
export function fmtIoTime(ms: number): string {
  const scale = pickTimeScale(ms)
  return `${fmtScaled(ms, scale)} ${TIME_UNITS[scale.unit]}`
}

export function fmtMetricTotal(value: number, mode: MetricMode): string {
  return mode === 'count' ? fmtCompact(value) : fmtIoTime(value)
}
