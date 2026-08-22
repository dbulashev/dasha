import { fmtCompact, fmtPct } from '@/utils/format'

export type MetricMode = 'count' | 'time'

// Raw pg_stat_io column names in display order. They are the labels too: the
// audience reads them natively and they match the payload keys.
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

// v1 shows buffered relation I/O only. PostgreSQL 18 adds WAL rows to the same
// view; they belong on a card of their own, not mixed into these totals.
export const VISIBLE_OBJECTS = ['relation', 'temp relation']

export const CONTEXTS = ['normal', 'vacuum', 'bulkread', 'bulkwrite'] as const

// Stable per-context colours, reused by every chart and card on the page.
export const CONTEXT_COLORS: Record<string, string> = {
  normal: '#2196F3',
  vacuum: '#FF9800',
  bulkread: '#4CAF50',
  bulkwrite: '#9C27B0',
  init: '#607D8B',
}

/** One cell of the matrix over the selected window. */
export interface MatrixRow {
  backend_type: string
  object: string
  context: string
  values: Record<string, number>
}

// The byte counters ride along with the counts: PostgreSQL 18 reports them
// directly and older versions derive them from op_bytes, so the difference
// never reaches here.
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

/** hits / (hits + reads); null when the context saw no buffer access at all. */
export function hitRatio(rows: MatrixRow[]): number | null {
  const hits = sumValues(rows, 'hits')
  const reads = sumValues(rows, 'reads')
  const total = hits + reads
  return total > 0 ? hits / total : null
}

/**
 * Share of the window spent inside an I/O call. pg_stat_io times are in
 * milliseconds and are summed across backends, so this can legitimately exceed
 * 1 — it is concurrency, not a percentage of wall clock.
 */
export function timeShare(ms: number, seconds: number): number | null {
  return seconds > 0 ? ms / (seconds * 1000) : null
}

export function fmtMetricRate(value: number, seconds: number, mode: MetricMode): string {
  if (seconds <= 0) return '—'
  if (mode === 'count') return `${fmtCompact(value / seconds)}/s`
  return fmtPct(timeShare(value, seconds)! * 100)
}

export function fmtMetricTotal(value: number, mode: MetricMode): string {
  return mode === 'count' ? fmtCompact(value) : `${fmtCompact(value)} ms`
}
