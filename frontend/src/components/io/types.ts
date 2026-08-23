import { fmtCompact, fmtScaled, pickTimeScale } from '@/utils/format'

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

// PostgreSQL 18 adds the WAL rows; they share the view but not the buffer
// cache, so the cards above the matrix stay on the relation objects.
export const VISIBLE_OBJECTS = ['relation', 'temp relation', 'wal']

export const BUFFER_OBJECTS = ['relation', 'temp relation']

export const CONTEXTS = ['normal', 'vacuum', 'bulkread', 'bulkwrite'] as const

// Categorical slots as [light, dark]; the dark step is picked for the dark
// surface, not derived from the light one. Validated for colour-vision
// deficiency: the four context hues clear the all-pairs floors in both modes,
// the eight-slot order clears the adjacent-pair floors used by a stacked chart.
const SERIES_SLOTS: readonly (readonly [string, string])[] = [
  ['#2a78d6', '#3987e5'],
  ['#eb6834', '#d95926'],
  ['#1baf7a', '#199e70'],
  ['#eda100', '#c98500'],
  ['#e87ba4', '#d55181'],
  ['#008300', '#008300'],
  ['#4a3aa7', '#9085e9'],
  ['#e34948', '#e66767'],
]

const NEUTRAL_SLOT: readonly [string, string] = ['#6d6c66', '#a8a79c']

// Colour follows the entity, not its rank: a filtered chart must not repaint
// the series that survived.
const CONTEXT_SLOTS: Record<string, readonly [string, string]> = {
  normal: SERIES_SLOTS[0],
  vacuum: SERIES_SLOTS[3],
  bulkread: SERIES_SLOTS[4],
  bulkwrite: SERIES_SLOTS[5],
  init: NEUTRAL_SLOT,
}

// The backend types pg_stat_io reports; anything outside this list folds into
// one neutral "other" series rather than inventing a ninth hue.
const BACKEND_TYPE_SLOTS: Record<string, number> = {
  'client backend': 0,
  'autovacuum worker': 1,
  // PostgreSQL 18 moves much of the reading into these.
  'io worker': 2,
  checkpointer: 3,
  'background writer': 4,
  'background worker': 5,
  startup: 6,
  walsender: 7,
}

export function paletteColor(index: number, dark: boolean): string {
  return (SERIES_SLOTS[index] ?? NEUTRAL_SLOT)[dark ? 1 : 0]
}

export function contextColor(context: string, dark: boolean): string {
  return (CONTEXT_SLOTS[context] ?? NEUTRAL_SLOT)[dark ? 1 : 0]
}

export function backendTypeColor(backendType: string, dark: boolean): string | null {
  const slot = BACKEND_TYPE_SLOTS[backendType]
  return slot === undefined ? null : paletteColor(slot, dark)
}

export function neutralColor(dark: boolean): string {
  return NEUTRAL_SLOT[dark ? 1 : 0]
}

/** Sequential ramp on one hue: transparent for idle, saturated for the peak. */
export function heatColor(intensity: number, dark: boolean): string {
  const rgb = dark ? '57, 135, 229' : '42, 120, 214'
  const clamped = Math.min(1, Math.max(0, intensity))
  return `rgba(${rgb}, ${(0.06 + 0.34 * clamped).toFixed(3)})`
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

// Suffixes for the units pickTimeScale returns; they sit next to raw
// pg_stat_io column names, so they stay English like the labels around them.
const TIME_UNITS: Record<string, string> = {
  us: 'µs',
  ms: 'ms',
  sec: 's',
  min: 'min',
  h: 'h',
}

/**
 * Rate over the window: operations per second, or milliseconds of I/O per
 * second of observation. The time rate can exceed one second per second — the
 * counters are summed across backends, so it is concurrency, not wall clock.
 */
export function fmtMetricRate(value: number, seconds: number, mode: MetricMode): string {
  if (seconds <= 0) return '—'

  const perSecond = value / seconds
  if (mode === 'count') return `${fmtCompact(perSecond)}/s`

  const scale = pickTimeScale(perSecond)
  return `${fmtScaled(perSecond, scale)} ${TIME_UNITS[scale.unit]}/s`
}

export function fmtMetricTotal(value: number, mode: MetricMode): string {
  return mode === 'count' ? fmtCompact(value) : `${fmtCompact(value)} ms`
}
