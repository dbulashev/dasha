import { describe, expect, it } from 'vitest'

import type { MaintenanceAutovacuumQueueRow } from '@/api/models/index'
import { componentValue, isDominant, scoreBar, startedByChips } from '../autovacuumQueue'

const row: MaintenanceAutovacuumQueueRow = {
  schema: 'public',
  table: 't',
  score: 2.5,
  xid_score: 2.5,
  mxid_score: 0.1,
  vacuum_score: 1.2,
  vacuum_insert_score: 0,
  analyze_score: 0.4,
  do_vacuum: true,
  do_analyze: false,
  for_wraparound: true,
  dominant: 'xid',
}

describe('autovacuum queue', () => {
  it('highlights only the dominant component', () => {
    expect(isDominant(row, 'xid')).toBe(true)
    expect(isDominant(row, 'vacuum')).toBe(false)
    expect(isDominant({ ...row, dominant: null }, 'xid')).toBe(false)
    expect(componentValue(row, 'vacuum')).toBe(1.2)
  })

  it('scales the bar to the page top but not below 1', () => {
    expect(scoreBar(0.5, 0.5)).toBe(50)
    expect(scoreBar(2.5, 5)).toBe(50)
  })
})

describe('startedByChips', () => {
  it('is null before PG 19', () => {
    expect(startedByChips(null, null, null)).toBeNull()
    expect(startedByChips(undefined, undefined)).toBeNull()
  })

  it('drops zero counts and flags wraparound', () => {
    const chips = startedByChips(0, 2, 1)!
    expect(chips.map((c) => c.key)).toEqual(['autovacuum', 'wraparound'])
    expect(chips[1].color).toBe('error')
  })
})
