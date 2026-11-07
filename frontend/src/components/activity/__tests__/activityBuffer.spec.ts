import { describe, expect, it } from 'vitest'

import type { ActivitySample } from '@/api/models/activitySample'
import type { ActivitySession } from '@/api/models/activitySession'
import { ActivityBuffer, barStepMs, BACKGROUND, eventValue, OTHER_EVENTS, queryKeyLabel } from '../activityBuffer'
import { CLASS_INDEX } from '../classes'

const T0 = Date.UTC(2026, 10, 4, 12, 0, 0)

function session(over: Partial<ActivitySession>): ActivitySession {
  return {
    pid: 1,
    backend_type: 'client backend',
    database: 'shop',
    user: 'app',
    application: 'api',
    client_addr: '10.0.0.1',
    state: 'active',
    class: 'CPU',
    wait_event: '',
    query_key: 'q:1',
    query_key_kind: 'query_id',
    query_id: '1',
    query_start: null,
    xact_start: null,
    holds_xmin: false,
    ...over,
  }
}

function sample(offsetSec: number, sessions: ActivitySession[]): ActivitySample {
  return {
    sampled_at: new Date(T0 + offsetSec * 1000).toISOString(),
    truncated: false,
    can_read_all_stats: true,
    query_id_available: true,
    track_utility: true,
    sessions,
  }
}

// Feeds a sample with the client clock equal to the server's.
function feed(buf: ActivityBuffer, s: ActivitySample) {
  buf.push(s, Date.parse(s.sampled_at))
}

const all = { from: T0 - 1000, to: T0 + 3_600_000 }

describe('ActivityBuffer', () => {
  it('evicts samples older than the window and releases their strings', () => {
    const buf = new ActivityBuffer(10_000)

    buf.push(sample(0, [session({ pid: 1, database: 'old', query_key: 'q:old', text: 'select old' })]))
    buf.push(sample(5, [session({ pid: 2, database: 'shop', query_key: 'q:1', text: 'select 1' })]))
    expect(buf.length).toBe(2)
    expect(buf.queryInfo('q:old')?.text).toBe('select old')

    buf.push(sample(11, [session({ pid: 2, database: 'shop', query_key: 'q:1' })]))

    expect(buf.length).toBe(2)
    expect(buf.first).toBe(T0 + 5000)
    expect(buf.queryInfo('q:old')).toBeUndefined()
    expect(buf.queryInfo('q:1')?.text).toBe('select 1')
    expect(buf.sessionInfo(1)).toBeUndefined()
    expect(buf.dictSizes()).toMatchObject({ databases: 1, keys: 1, queries: 1, sessions: 1 })
  })

  it('keeps the text of a key that arrives later without it', () => {
    const buf = new ActivityBuffer(60_000)

    buf.push(sample(0, [session({ query_key: 'q:7' })]))
    expect(buf.queryInfo('q:7')?.text).toBe('')

    buf.push(sample(1, [session({ query_key: 'q:7', text: 'select 7' })]))
    buf.push(sample(2, [session({ query_key: 'q:7' })]))
    expect(buf.queryInfo('q:7')?.text).toBe('select 7')
  })

  it('shows background processes only when asked, without dropping them', () => {
    const buf = new ActivityBuffer(60_000)
    const bg = session({ pid: 9, backend_type: 'autovacuum worker', class: 'IO', query_key: 'q:av' })

    feed(buf, sample(0, [session({ pid: 1 }), bg]))
    feed(buf, sample(1, [session({ pid: 1 }), bg]))

    const sum = (byEvent: boolean) =>
      buf
        .chart(all, {}, new Set(), byEvent)
        .series.reduce((n, s) => n + s.data.reduce<number>((a, v) => a + (v ?? 0), 0), 0)

    expect(sum(false)).toBe(1)
    expect(buf.aggregate(all, {}, 'query').map((r) => r.value)).toEqual(['q:1'])
    expect(buf.pidsWithClass(all, 'IO').size).toBe(0)

    buf.includeBackground = true
    expect(sum(false)).toBe(2)
    expect(sum(true)).toBe(2)
    expect(buf.aggregate(all, {}, 'query').map((r) => r.value).sort()).toEqual(['q:1', 'q:av'])
    expect(buf.pidsWithClass(all, 'IO')).toEqual(new Set([9]))
    expect(buf.length).toBe(2)
  })

  it('puts background processes with empty values in their own row', () => {
    const buf = new ActivityBuffer(60_000)
    buf.includeBackground = true
    const checkpointer = session({
      pid: 9,
      backend_type: 'checkpointer',
      class: 'Timeout',
      query_key: '',
      query_key_kind: 'none',
      database: '',
      user: '',
      application: '',
    })
    const hidden = session({ pid: 2, query_key: '', query_key_kind: 'none', application: '' })

    feed(buf, sample(0, [session({ pid: 1 }), hidden, checkpointer]))

    const values = (dim: 'query' | 'database' | 'user' | 'application') =>
      buf.aggregate(all, {}, dim).map((r) => r.value).sort()
    expect(values('query')).toEqual(['', 'q:1', BACKGROUND].sort())
    expect(values('application')).toEqual(['', 'api', BACKGROUND].sort())
    expect(values('database')).toEqual(['shop', BACKGROUND].sort())
    expect(values('user')).toEqual(['app', BACKGROUND].sort())
  })

  it('counts a parallel worker as client load', () => {
    const buf = new ActivityBuffer(60_000)
    const worker = session({ pid: 2, backend_type: 'parallel worker', query_key: 'q:1' })

    feed(buf, sample(0, [session({ pid: 1 }), worker]))

    expect(buf.aggregate(all, {}, 'query').map((r) => r.value)).toEqual(['q:1'])
    expect(buf.aggregate(all, {}, 'session')).toHaveLength(2)
  })

  it('skips a repeat of the previous sample', () => {
    const buf = new ActivityBuffer(60_000)

    feed(buf, sample(0, [session({ pid: 1 })]))
    feed(buf, sample(0, [session({ pid: 1 })]))
    expect(buf.length).toBe(1)

    feed(buf, sample(1, [session({ pid: 1 })]))
    expect(buf.length).toBe(2)

    buf.clear()
    feed(buf, sample(1, [session({ pid: 1 })]))
    expect(buf.length).toBe(1)
  })

  it('aggregates AAS and shares over measured samples only', () => {
    const buf = new ActivityBuffer(60_000)

    feed(buf, sample(0, [session({ pid: 1 }), session({ pid: 2, class: 'Lock', wait_event: 'relation', query_key: 'q:2' })]))
    buf.pushGap(T0 + 1000)
    feed(buf, sample(2, [session({ pid: 1 })]))

    const rows = buf.aggregate(all, {}, 'query')
    expect(rows.map((r) => r.value)).toEqual(['q:1', 'q:2'])
    expect(rows[0].aas).toBe(1)
    expect(rows[1].aas).toBe(0.5)
    expect(rows[0].sharePct).toBeCloseTo(200 / 3)
    expect(rows[1].byClass[CLASS_INDEX.Lock]).toBe(1)
  })

  it('ANDs dimensions and ORs values within one', () => {
    const buf = new ActivityBuffer(60_000)

    buf.push(
      sample(0, [
        session({ pid: 1, database: 'shop', user: 'app' }),
        session({ pid: 2, database: 'crm', user: 'app' }),
        session({ pid: 3, database: 'crm', user: 'etl' }),
      ]),
    )

    const by = (filters: Parameters<ActivityBuffer['aggregate']>[1]) =>
      buf.aggregate(all, filters, 'session').map((r) => r.value)

    expect(by({ database: new Set(['crm']), user: new Set(['app']) })).toEqual(['2'])
    expect(by({ database: new Set(['shop', 'crm']), user: new Set(['etl']) })).toEqual(['3'])
    expect(by({ database: new Set(['nowhere']) })).toEqual([])
    expect(by({ event: new Set([eventValue('CPU', '')]) })).toHaveLength(3)
  })

  it('hides a class from aggregates and the chart', () => {
    const buf = new ActivityBuffer(60_000)
    const idleTx = CLASS_INDEX.ClientIdleTx

    buf.push(sample(0, [session({ pid: 1 }), session({ pid: 2, class: 'ClientIdleTx', state: 'idle in transaction' })]))

    expect(buf.aggregate(all, {}, 'session', new Set([idleTx])).map((r) => r.value)).toEqual(['1'])

    const chart = buf.chart({ from: T0, to: T0 + 1000 }, {}, new Set([idleTx]), false)
    expect(chart.series.map((s) => s.key)).not.toContain('ClientIdleTx')
  })

  it('draws a bar holding only gaps or nothing as null', () => {
    const buf = new ActivityBuffer(60_000)

    feed(buf, sample(0, [session({ pid: 1 }), session({ pid: 2 })]))
    buf.pushGap(T0 + 1000)
    feed(buf, sample(3, [session({ pid: 1 })]))

    const chart = buf.chart({ from: T0, to: T0 + 4000 }, {}, new Set(), false)
    const cpu = chart.series.find((s) => s.key === 'CPU')!

    expect(chart.stepMs).toBe(1000)
    expect(cpu.data).toEqual([2, null, null, 1])
  })

  it('uses the same numbers with and without the per-class shortcut', () => {
    const buf = new ActivityBuffer(60_000)

    buf.push(sample(0, [session({ pid: 1 }), session({ pid: 2, class: 'IO', wait_event: 'DataFileRead' })]))
    buf.push(sample(1, [session({ pid: 1, class: 'IO', wait_event: 'DataFileRead' })]))

    const range = { from: T0, to: T0 + 2000 }
    const plain = buf.chart(range, {}, new Set(), false)
    const filtered = buf.chart(range, { database: new Set(['shop']) }, new Set(), false)

    expect(filtered.series).toEqual(plain.series)
  })

  it('splits the chart by the top events and folds the rest', () => {
    const buf = new ActivityBuffer(60_000)

    buf.push(
      sample(0, [
        session({ pid: 1, class: 'IO', wait_event: 'DataFileRead' }),
        session({ pid: 2, class: 'IO', wait_event: 'DataFileRead' }),
        session({ pid: 3, class: 'IO', wait_event: 'DataFileRead' }),
        session({ pid: 4, class: 'LWLock', wait_event: 'WALWrite' }),
        session({ pid: 5, class: 'LWLock', wait_event: 'WALWrite' }),
        session({ pid: 6, class: 'Lock', wait_event: 'relation' }),
      ]),
    )

    const chart = buf.chart({ from: T0, to: T0 + 1000 }, {}, new Set(), true, 2)

    expect(chart.series.map((s) => s.key)).toEqual([
      eventValue('IO', 'DataFileRead'),
      eventValue('LWLock', 'WALWrite'),
      OTHER_EVENTS,
    ])
    expect(chart.series[2].data).toEqual([1])
  })

  it('finds the sessions that waited on a lock', () => {
    const buf = new ActivityBuffer(60_000)

    buf.push(sample(0, [session({ pid: 1 }), session({ pid: 2, class: 'Lock', wait_event: 'tuple' })]))

    expect([...buf.pidsWithClass(all, 'Lock')]).toEqual([2])
  })

  it('places a gap on the server clock and skips it before the first sample', () => {
    const buf = new ActivityBuffer(60_000)
    const skew = 90_000

    buf.pushGap(T0)
    expect(buf.length).toBe(0)

    buf.push(sample(0, [session({})]), T0 + skew)
    buf.pushGap(T0 + skew + 1000)

    expect(buf.length).toBe(2)
    expect(buf.last).toBe(T0 + 1000)
  })

  it('clears everything', () => {
    const buf = new ActivityBuffer(60_000)

    buf.push(sample(0, [session({ text: 'select 1' })]))
    buf.clear()

    expect(buf.length).toBe(0)
    expect(buf.queryInfo('q:1')).toBeUndefined()
    expect(buf.dictSizes()).toMatchObject({ keys: 0, sessions: 0 })
  })
})

describe('queryKeyLabel', () => {
  it('drops the kind prefix', () => {
    expect(queryKeyLabel('q:-8886085649470066503')).toBe('-8886085649470066503')
    expect(queryKeyLabel('f:3f2a9c')).toBe('3f2a9c')
    expect(queryKeyLabel('t:00ab')).toBe('00ab')
    expect(queryKeyLabel('')).toBe('')
  })
})

describe('barStepMs', () => {
  it('picks the first step that keeps at most 300 bars', () => {
    expect(barStepMs(5 * 60_000)).toBe(1000)
    expect(barStepMs(10 * 60_000)).toBe(2000)
    expect(barStepMs(60 * 60_000)).toBe(15_000)
    expect(barStepMs(240 * 60_000)).toBe(60_000)
  })

  it('never goes below the sampling period', () => {
    expect(barStepMs(5 * 60_000, 5000)).toBe(5000)
    expect(barStepMs(5 * 60_000, 10_000)).toBe(10_000)
    expect(barStepMs(60 * 60_000, 5000)).toBe(15_000)
  })
})
