import type { LockStatsHistory, LockStatsPoint } from '@/api/models/index'
import { neutralColor, paletteColor } from '@/utils/seriesColors'

export type LockMetric = 'waits' | 'wait_time'

// The common lock types keep a hue of their own; the rare ones share the neutral one.
const LOCK_TYPE_SLOTS: Record<string, number> = {
  relation: 0,
  transactionid: 1,
  tuple: 2,
  advisory: 3,
  extend: 4,
  virtualxid: 5,
  object: 6,
  page: 7,
}

export function lockTypeColor(locktype: string, dark: boolean): string {
  const slot = LOCK_TYPE_SLOTS[locktype]
  return slot === undefined ? neutralColor(dark) : paletteColor(slot, dark)
}

// Only relation locks take the fast path; other lock types have no counter to show.
export function hasFastpath(locktype: string): boolean {
  return locktype === 'relation'
}

export interface LockStatsRow {
  locktype: string
  waits: number
  waitTimeMs: number
  avgWaitMs: number | null
  fastpathExceeded: number
  // Share of the window's total wait time.
  sharePct: number | null
}

// Totals over the window, longest waits first.
export function windowRows(history: LockStatsHistory | null): LockStatsRow[] {
  const rows = (history?.series ?? []).map((s) => {
    let waits = 0
    let waitTimeMs = 0
    let fastpathExceeded = 0
    for (const p of s.points) {
      waits += p.waits
      waitTimeMs += p.wait_time_ms
      fastpathExceeded += p.fastpath_exceeded
    }
    return {
      locktype: s.locktype,
      waits,
      waitTimeMs,
      avgWaitMs: waits > 0 ? waitTimeMs / waits : null,
      fastpathExceeded,
      sharePct: null as number | null,
    }
  })

  const totalTime = rows.reduce((acc, r) => acc + r.waitTimeMs, 0)
  for (const r of rows) r.sharePct = totalTime > 0 ? (r.waitTimeMs / totalTime) * 100 : null

  return rows.sort(
    (a, b) => b.waitTimeMs - a.waitTimeMs || b.waits - a.waits || a.locktype.localeCompare(b.locktype),
  )
}

// Measured length of the window: every series carries the same buckets.
export function windowSeconds(history: LockStatsHistory | null): number {
  const points = history?.series?.[0]?.points ?? []
  return points.reduce((acc, p) => acc + p.duration_seconds, 0)
}

export function windowPartial(history: LockStatsHistory | null): boolean {
  return (history?.series ?? []).some((s) => s.points.some((p) => !p.complete))
}

// An incomplete bucket is a gap, not a zero.
export function pointValue(p: LockStatsPoint, metric: LockMetric): number | null {
  if (!p.complete) return null
  return metric === 'waits' ? p.waits : p.wait_time_ms
}

export function fastpathExceeded(rows: LockStatsRow[]): boolean {
  return rows.some((r) => r.fastpathExceeded > 0)
}
