import { describe, expect, it } from 'vitest'

import type { Cluster } from '@/api/models'
import {
  clusterSectionLocation,
  dbLocation,
  hostLocation,
  placeOf,
  resolveSection,
  sectionLocation,
  type Place,
} from '@/utils/palette/location'
import { MAIN_SECTION, sectionBySlug } from '@/utils/palette/sections'

const s = (slug: string) => sectionBySlug(slug)!

const tableDescribe: Place = {
  section: s('table-describe'),
  cluster: 'prod-billing',
  query: { host: 'pg-02', db: 'app', schema: 'public', table: 'orders', snapshot: '7' },
}

const other: Cluster = {
  name: 'prod-auth',
  instances: [{}, { host_name: 'auth-2' }, { host_name: 'auth-1' }],
  databases: ['users', 'audit'],
}

describe('placeOf', () => {
  it('reads the section, cluster and query from a route', () => {
    expect(placeOf({ name: 'HealthScore', params: { clustername: 'c' }, query: { host: 'h' } })).toEqual({
      section: s('health-score'),
      cluster: 'c',
      query: { host: 'h' },
    })
  })

  it('has no section on Fleet and no cluster without the param', () => {
    expect(placeOf({ name: 'fleet', params: {}, query: {} })).toEqual({ section: null, cluster: null, query: {} })
    expect(placeOf({ name: 'home', params: { clustername: '' }, query: {} }).cluster).toBeNull()
  })
})

describe('hostLocation', () => {
  it('keeps the section and every other query parameter', () => {
    expect(hostLocation(tableDescribe, 'pg-01', false)).toEqual({
      name: 'table-describe',
      params: { clustername: 'prod-billing' },
      query: { host: 'pg-01', db: 'app', schema: 'public', table: 'orders', snapshot: '7' },
    })
  })

  it('sends a replica from a primary-only section to the summary', () => {
    const place: Place = { section: s('maintenance'), cluster: 'c', query: { host: 'pg-01', db: 'app' } }
    expect(hostLocation(place, 'pg-02', true)).toEqual({
      name: 'home',
      params: { clustername: 'c' },
      query: { host: 'pg-02', db: 'app' },
    })
    expect(hostLocation(place, 'pg-02', false)).toMatchObject({ name: 'maintenance' })
  })

  it('lands on the summary from Fleet', () => {
    const place: Place = { section: null, cluster: 'c', query: {} }
    expect(hostLocation(place, 'pg-01', false)).toMatchObject({ name: 'home', query: { host: 'pg-01' } })
  })
})

describe('dbLocation', () => {
  it('keeps the section and the rest of the query', () => {
    expect(dbLocation(tableDescribe, 'other')).toEqual({
      name: 'table-describe',
      params: { clustername: 'prod-billing' },
      query: { host: 'pg-02', db: 'other', schema: 'public', table: 'orders', snapshot: '7' },
    })
  })
})

describe('sectionLocation', () => {
  it('carries host and db only, like a menu link', () => {
    expect(sectionLocation(tableDescribe, s('locks'))).toEqual({
      name: 'locks',
      params: { clustername: 'prod-billing' },
      query: { host: 'pg-02', db: 'app' },
    })
  })

  it('carries the section keepQuery too', () => {
    expect(sectionLocation({ ...tableDescribe, section: s('tables') }, s('table-describe')).query).toEqual({
      host: 'pg-02',
      db: 'app',
      schema: 'public',
      table: 'orders',
    })
  })

  it('omits parameters missing from the URL', () => {
    expect(sectionLocation({ section: s('locks'), cluster: 'c', query: {} }, s('io')).query).toEqual({})
  })
})

describe('clusterSectionLocation', () => {
  it('resets the query to the defaults of another cluster', () => {
    expect(clusterSectionLocation(tableDescribe, other, s('table-describe'))).toEqual({
      name: 'table-describe',
      params: { clustername: 'prod-auth' },
      query: { host: 'auth-2', db: 'users' },
    })
  })

  it('behaves as a section link in the current cluster', () => {
    const current: Cluster = { name: 'prod-billing', instances: [{ host_name: 'pg-01' }], databases: ['x'] }
    expect(clusterSectionLocation(tableDescribe, current, s('locks'))).toEqual(
      sectionLocation(tableDescribe, s('locks')),
    )
  })

  it('leaves out a host or database the cluster lacks', () => {
    expect(clusterSectionLocation(tableDescribe, { name: 'empty' }, s('locks')).query).toEqual({})
  })
})

describe('resolveSection', () => {
  it('keeps a visible section', () => {
    expect(resolveSection(s('locks'), () => true)).toEqual({ section: s('locks'), fallback: false })
  })

  it('falls back to the summary for a hidden one', () => {
    expect(resolveSection(s('logs'), () => false)).toEqual({ section: MAIN_SECTION, fallback: true })
  })

  it('lands on the summary from Fleet', () => {
    expect(resolveSection(null, () => false)).toEqual({ section: MAIN_SECTION, fallback: false })
  })
})
