import { describe, it, expect } from 'vitest'
import { foreignStatsSource } from '../autosnapshot'

describe('foreignStatsSource', () => {
  it('says nothing when the snapshot uses the source in use now', () => {
    expect(foreignStatsSource('pg_stat_statements', 'pg_stat_statements')).toBe('')
  })

  it('names the extension when it differs', () => {
    expect(foreignStatsSource('pgpro_stats', 'pg_stat_statements')).toBe('pgpro_stats')
  })

  it('says nothing while the current source is unknown', () => {
    expect(foreignStatsSource('pgpro_stats', '')).toBe('')
    expect(foreignStatsSource('pgpro_stats', null)).toBe('')
    expect(foreignStatsSource('pgpro_stats', undefined)).toBe('')
  })

  it('says nothing for a snapshot with no recorded source', () => {
    expect(foreignStatsSource(null, 'pgpro_stats')).toBe('')
    expect(foreignStatsSource(undefined, 'pgpro_stats')).toBe('')
    expect(foreignStatsSource('', 'pgpro_stats')).toBe('')
  })
})
