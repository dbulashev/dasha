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

export function fmtPct(v: number | null | undefined, decimals = 1): string {
  if (v == null) return '—'
  return v.toFixed(decimals) + '%'
}

export function fmtInt(v: number | null | undefined): string {
  if (v == null) return '—'
  return v.toLocaleString()
}
