import { describe, expect, it } from 'vitest'

import type { LockStatsHistory, LockStatsPoint } from '@/api/models/index'
import { fastpathExceeded, pointValue, windowPartial, windowRows, windowSeconds } from '../lockStats'

function point(over: Partial<LockStatsPoint> = {}): LockStatsPoint {
  return {
    from: '2026-11-28T10:00:00Z',
    to: '2026-11-28T11:00:00Z',
    duration_seconds: 3600,
    complete: true,
    waits: 0,
    wait_time_ms: 0,
    fastpath_exceeded: 0,
    ...over,
  }
}

function history(series: Record<string, LockStatsPoint[]>): LockStatsHistory {
  return {
    meta: {
      instance: 'h1',
      version_changed: false,
      deadlock_timeout_changed: false,
      max_locks_per_transaction_changed: false,
    },
    series: Object.entries(series).map(([locktype, points]) => ({ locktype, points })),
  }
}

describe('windowRows', () => {
  it('sums the window, averages per wait and shares the time', () => {
    const rows = windowRows(
      history({
        advisory: [point({ waits: 17, wait_time_ms: 2400 })],
        relation: [point({ waits: 1000, wait_time_ms: 30000 }), point({ waits: 204, wait_time_ms: 8100 })],
      }),
    )

    expect(rows.map((r) => r.locktype)).toEqual(['relation', 'advisory'])
    expect(rows[0].waits).toBe(1204)
    expect(rows[0].waitTimeMs).toBe(38100)
    expect(rows[0].avgWaitMs).toBeCloseTo(31.64, 2)
    expect(rows[0].sharePct! + rows[1].sharePct!).toBeCloseTo(100)
  })

  it('has no average and no share without waits', () => {
    const [row] = windowRows(history({ tuple: [point()] }))
    expect(row.avgWaitMs).toBeNull()
    expect(row.sharePct).toBeNull()
  })
})

describe('series helpers', () => {
  it('draws an incomplete bucket as a gap', () => {
    expect(pointValue(point({ complete: false, waits: 5 }), 'waits')).toBeNull()
    expect(pointValue(point({ waits: 5, wait_time_ms: 70 }), 'wait_time')).toBe(70)
  })

  it('measures the window by the first series', () => {
    const h = history({ relation: [point(), point({ duration_seconds: 0, complete: false })] })
    expect(windowSeconds(h)).toBe(3600)
    expect(windowPartial(h)).toBe(true)
    expect(windowSeconds(null)).toBe(0)
  })

  it('asks for max_locks_per_transaction only when the fast path overflowed', () => {
    expect(fastpathExceeded(windowRows(history({ relation: [point()] })))).toBe(false)
    expect(fastpathExceeded(windowRows(history({ relation: [point({ fastpath_exceeded: 3 })] })))).toBe(true)
  })
})
