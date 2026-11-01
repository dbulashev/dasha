import { describe, expect, it } from 'vitest'
import type { Cluster } from '@/api/models'
import { hostItems, primaryOf, standbysOf } from '@/utils/clusters'

describe('hostItems', () => {
  it('uses the display name as the title and keeps the host name as the value', () => {
    const cluster: Cluster = {
      name: 'etcd_alpha',
      instances: [
        { host_name: '10.0.0.12', display_name: 'alpha-2' },
        { host_name: '10.0.0.11', display_name: 'alpha-1' },
      ],
    }
    expect(hostItems(cluster)).toEqual([
      { value: '10.0.0.11', title: 'alpha-1', address: '10.0.0.11' },
      { value: '10.0.0.12', title: 'alpha-2', address: '10.0.0.12' },
    ])
  })

  it('falls back to the host name and sorts by the shown title', () => {
    const cluster: Cluster = {
      instances: [
        { host_name: 'pg-2' },
        { host_name: '10.0.0.1', display_name: 'pg-1' },
        { host_name: 'pg-3', display_name: 'pg-3' },
        {},
      ],
    }
    expect(hostItems(cluster)).toEqual([
      { value: '10.0.0.1', title: 'pg-1', address: '10.0.0.1' },
      { value: 'pg-2', title: 'pg-2' },
      { value: 'pg-3', title: 'pg-3' },
    ])
  })

  it('is empty without a cluster', () => {
    expect(hostItems(null)).toEqual([])
    expect(hostItems({ name: 'x' })).toEqual([])
  })
})

describe('standby links', () => {
  const clusters: Cluster[] = [
    { name: 'beta' },
    { name: 'beta-standby', standby_of: 'beta' },
    { name: 'beta-dr', standby_of: 'beta' },
    { name: 'orphan', standby_of: 'rejected-duplicate' },
    { name: 'self', standby_of: 'self' },
  ]

  it('finds the primary of a standby', () => {
    expect(primaryOf(clusters, 'beta-standby')).toBe('beta')
    expect(primaryOf(clusters, 'beta')).toBeNull()
  })

  it('hides a primary that is not in the list', () => {
    expect(primaryOf(clusters, 'orphan')).toBeNull()
  })

  it('ignores a self-reference', () => {
    expect(primaryOf(clusters, 'self')).toBeNull()
    expect(standbysOf(clusters, 'self')).toEqual([])
  })

  it('lists standbys of a primary, sorted', () => {
    expect(standbysOf(clusters, 'beta')).toEqual(['beta-dr', 'beta-standby'])
    expect(standbysOf(clusters, 'beta-standby')).toEqual([])
    expect(standbysOf(null, 'beta')).toEqual([])
    expect(standbysOf(clusters, null)).toEqual([])
  })
})
