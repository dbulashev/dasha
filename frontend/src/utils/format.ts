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

export function fmtRowCount(n: number): string {
  if (n >= 1_000_000_000) return parseFloat((n / 1_000_000_000).toFixed(1)) + 'B'
  if (n >= 1_000_000) return parseFloat((n / 1_000_000).toFixed(1)) + 'M'
  if (n >= 1_000) return parseFloat((n / 1_000).toFixed(1)) + 'K'
  return n.toLocaleString()
}
