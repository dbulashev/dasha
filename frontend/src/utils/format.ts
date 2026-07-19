import { useLocaleStore } from '@/stores/locale'
import { usePrefsStore } from '@/stores/prefs'
import { isValidTimeZone, tzSuffix } from '@/constants/timezones'

export function fmtLag(seconds: number | null | undefined): string {
  if (seconds == null || seconds === 0) return '0 s'
  if (seconds < 1) return `${Math.round(seconds * 1000)} ms`
  if (seconds < 60) return `${seconds.toFixed(1)} s`
  return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)} s`
}

export function fmtBytes(bytes: number | null | undefined): string {
  if (bytes == null) return '—'
  if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(1) + ' GB'
  if (bytes >= 1048576) return (bytes / 1048576).toFixed(1) + ' MB'
  if (bytes >= 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return bytes + ' B'
}

type TFunc = (key: string) => string

export function fmtMs(ms: number | null | undefined, t: TFunc): string {
  if (ms == null) return '—'
  if (Math.abs(ms) >= 3600000) {
    const h = Math.floor(Math.abs(ms) / 3600000)
    const m = Math.floor((Math.abs(ms) % 3600000) / 60000)
    const sign = ms < 0 ? '-' : ''
    return m > 0 ? `${sign}${h} ${t('time.h')} ${m} ${t('time.min')}` : `${sign}${h} ${t('time.h')}`
  }
  if (Math.abs(ms) >= 60000) {
    const m = Math.floor(Math.abs(ms) / 60000)
    const s = Math.floor((Math.abs(ms) % 60000) / 1000)
    const sign = ms < 0 ? '-' : ''
    return s > 0 ? `${sign}${m} ${t('time.min')} ${s} ${t('time.sec')}` : `${sign}${m} ${t('time.min')}`
  }
  if (Math.abs(ms) >= 1000) return `${(ms / 1000).toFixed(1)} ${t('time.sec')}`
  if (Math.abs(ms) >= 1) return `${ms.toFixed(1)} ${t('time.ms')}`
  if (Math.abs(ms) >= 0.001) return `${(ms * 1000).toFixed(1)} ${t('time.us')}`
  return `0 ${t('time.ms')}`
}

export interface TimeScale {
  divisor: number
  unit: string
}

export function pickTimeScale(maxMs: number): TimeScale {
  const abs = Math.abs(maxMs || 0)
  if (abs >= 3600000) return { divisor: 3600000, unit: 'h' }
  if (abs >= 60000) return { divisor: 60000, unit: 'min' }
  if (abs >= 1000) return { divisor: 1000, unit: 'sec' }
  return { divisor: 1, unit: 'ms' }
}

export function fmtScaled(ms: number, scale: TimeScale): string {
  const v = ms / scale.divisor
  if (Number.isInteger(v)) return String(v)
  return v.toFixed(1)
}

export function fmtPct(v: number | null | undefined, decimals = 1): string {
  if (v == null) return '—'
  return v.toFixed(decimals) + '%'
}

export function fmtInt(v: number | null | undefined): string {
  if (v == null) return '—'
  return v.toLocaleString()
}

// fmtCompact shortens large counters with k/M/B suffixes (locale-independent,
// matching the fmtBytes style). Values below 10k stay exact — precision only
// gets dropped where the digits stop being readable.
export function fmtCompact(v: number | null | undefined): string {
  if (v == null) return '—'
  const abs = Math.abs(v)
  if (abs >= 1e9) return trimZero((v / 1e9).toFixed(1)) + 'B'
  if (abs >= 1e6) return trimZero((v / 1e6).toFixed(1)) + 'M'
  if (abs >= 1e4) return trimZero((v / 1e3).toFixed(1)) + 'k'
  return fmtInt(v)
}

function trimZero(s: string): string {
  return s.endsWith('.0') ? s.slice(0, -2) : s
}

/**
 * Intl settings for every rendered timestamp: the language the user picked (not
 * the browser's, which is what a bare toLocaleString() would use) and their
 * timezone choice. Reading the stores here — rather than passing them in — keeps
 * the ~30 call sites unchanged, and the reactive reads still re-render templates
 * when either setting changes.
 */
// The persisted zone, or 'local' when it is unset or no longer valid — never a
// value that would make Intl throw.
function activeZone(): string {
  const tz = usePrefsStore().timezone

  return tz !== 'local' && isValidTimeZone(tz) ? tz : 'local'
}

function dateIntl(): { locale: string; options: Intl.DateTimeFormatOptions } {
  const locale = useLocaleStore().currentLocale().replace('_', '-') // ru_RU -> ru-RU
  const options: Intl.DateTimeFormatOptions = {}
  const tz = activeZone()

  if (tz !== 'local') {
    options.timeZone = tz
  }

  return { locale, options }
}

// Timestamps in a fixed zone are labelled with it, because a bare number in a
// different zone than the reader expects is worse than no number at all. The
// local zone needs no label: it is what an unlabelled time already means.
function zoneSuffix(): string {
  const tz = activeZone()

  return tz === 'local' ? '' : tzSuffix(tz)
}

function parseDate(iso: string | null | undefined): Date | null {
  if (!iso) return null
  const d = new Date(iso)
  // Pre-2000 dates are epoch-zero placeholders PostgreSQL reports as "never".
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return null
  return d
}

/**
 * Format an ISO timestamp in the user's language and timezone. Returns `empty`
 * for missing/unparsable values and for epoch-zero placeholders.
 */
export function fmtDateTime(iso: string | null | undefined, empty = '—'): string {
  const d = parseDate(iso)
  if (!d) return empty
  const { locale, options } = dateIntl()
  return d.toLocaleString(locale, options) + zoneSuffix()
}

/** Date-only variant of fmtDateTime (no clock time). */
export function fmtDate(iso: string | null | undefined, empty = '—'): string {
  const d = parseDate(iso)
  if (!d) return empty
  const { locale, options } = dateIntl()
  return d.toLocaleDateString(locale, options) + zoneSuffix()
}

/**
 * Compact axis label for time-series charts. `withDate` adds day/month for spans
 * longer than a day. Follows the same timezone setting as the tables, so chart
 * ticks and rows cannot disagree about what "10:00" means.
 */
export function fmtChartTime(value: string | number | Date, withDate: boolean): string {
  const d = value instanceof Date ? value : new Date(value)
  if (isNaN(d.getTime())) return ''
  const { locale, options } = dateIntl()

  const time: Intl.DateTimeFormatOptions = { ...options, hour: '2-digit', minute: '2-digit' }
  if (!withDate) return d.toLocaleTimeString(locale, time)

  return d.toLocaleString(locale, { ...time, month: '2-digit', day: '2-digit' })
}

export function fmtAge(createdAt: string | null | undefined, statsReset: string | null | undefined, unknownLabel = '?'): string {
  if (!createdAt || !statsReset) return unknownLabel
  const diff = new Date(createdAt).getTime() - new Date(statsReset).getTime()
  if (diff < 0) return '—'
  let remaining = Math.floor(diff / 1000)
  const days = Math.floor(remaining / 86400)
  remaining %= 86400
  const hrs = Math.floor(remaining / 3600)
  remaining %= 3600
  const min = Math.floor(remaining / 60)
  const sec = remaining % 60
  const parts: string[] = []
  if (days > 0) parts.push(`${days}d`)
  if (hrs > 0) parts.push(`${hrs}h`)
  if (min > 0) parts.push(`${min}m`)
  if (sec > 0 || parts.length === 0) parts.push(`${sec}s`)
  return parts.join(' ')
}

/**
 * Round a number to at most `decimals` places, dropping trailing zeros
 * (5 → 5, 90.22492448754167 → 90.22, 4.571607 → 4.57). Non-finite numbers and
 * non-number values pass through unchanged, so it is safe to map over a mixed
 * context object of strings/booleans/numbers (e.g. a recommendation's context).
 */
export function fmtNum(value: unknown, decimals = 2): unknown {
  if (typeof value !== 'number' || !Number.isFinite(value)) return value
  if (Number.isInteger(value)) return value
  return Number(value.toFixed(decimals))
}

export function fmtRowCount(n: number): string {
  if (n >= 1_000_000_000) return parseFloat((n / 1_000_000_000).toFixed(1)) + 'B'
  if (n >= 1_000_000) return parseFloat((n / 1_000_000).toFixed(1)) + 'M'
  if (n >= 1_000) return parseFloat((n / 1_000).toFixed(1)) + 'K'
  return n.toLocaleString()
}
