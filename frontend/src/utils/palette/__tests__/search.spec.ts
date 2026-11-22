import { describe, expect, it } from 'vitest'

import type { Cluster } from '@/api/models'
import { hostItems } from '@/utils/clusters'
import {
  initialIndex,
  LEVEL_EXACT,
  LEVEL_FUZZY,
  LEVEL_PREFIX,
  LEVEL_SUBSTRING,
  matchField,
  normalize,
  prepareField,
  RESULT_LIMIT,
  search,
  tokenize,
  type SearchInput,
  type SearchResult,
} from '@/utils/palette/search'
import { buildSectionIndex, isSectionVisible, type SectionDef } from '@/utils/palette/sections'
import { translator } from './helpers'

const ru = buildSectionIndex(translator('ru_RU'))
const en = buildSectionIndex(translator('en_US'))

const CLUSTERS: Cluster[] = [
  { name: 'prod-billing', supports_logs: false },
  { name: 'prod-auth', supports_logs: true },
  { name: 'billing-eu', supports_logs: true },
  { name: 'stage' },
]

// The palette's rule for search targets, minus the stores: only the logs flag
// differs between the test clusters.
function visibleIn(clusters: Cluster[]) {
  return (cluster: string) => (section: SectionDef) =>
    isSectionVisible(section, {
      current: false,
      versionNum: null,
      replica: false,
      snapshots: true,
      autosnapshot: true,
      logs: clusters.find(c => c.name === cluster)?.supports_logs === true,
    })
}

function input(query: string, over: Partial<SearchInput> = {}): SearchInput {
  return {
    query,
    clusters: CLUSTERS.map(c => prepareField(c.name!)),
    sections: ru,
    hosts: [],
    dbs: [],
    currentCluster: 'prod-auth',
    currentSection: 'main',
    currentHost: null,
    currentDb: null,
    visibleIn: visibleIn(CLUSTERS),
    recent: [],
    ...over,
  }
}

const label = (r: SearchResult) =>
  r.kind === 'section'
    ? `section:${r.section!.slug}`
    : r.kind === 'cluster-section'
      ? `cs:${r.cluster}:${r.section!.slug}`
      : r.kind === 'cluster'
        ? `cluster:${r.cluster}`
        : r.kind === 'host'
          ? `host:${r.host!.value}`
          : `db:${r.db}`

const labels = (q: string, over?: Partial<SearchInput>) => search(input(q, over)).map(label)

describe('normalize and tokenize', () => {
  it('lowercases, folds ё and keeps the length', () => {
    expect(normalize('ЁЛКА Mixed')).toBe('елка mixed')
    expect(normalize('İx').length).toBe('İx'.length)
  })

  it('splits on spaces and arrows, dropping empty tokens', () => {
    expect(tokenize('  Таблицы → Описание  ')).toEqual(['таблицы', 'описание'])
    expect(tokenize('   ')).toEqual([])
  })
})

describe('matchField', () => {
  const field = (s: string) => prepareField(s)

  it('ranks exact over prefix over substring over fuzzy', () => {
    expect(matchField('billing', field('billing'))).toEqual({ level: LEVEL_EXACT, ranges: [[0, 7]] })
    expect(matchField('billing', field('prod-billing-eu'))).toEqual({ level: LEVEL_EXACT, ranges: [[5, 12]] })
    expect(matchField('bill', field('prod-billing'))).toEqual({ level: LEVEL_PREFIX, ranges: [[5, 9]] })
    expect(matchField('billing', field('prodbilling'))).toEqual({ level: LEVEL_SUBSTRING, ranges: [[4, 11]] })
    expect(matchField('blng', field('billing'))?.level).toBe(LEVEL_FUZZY)
    expect(matchField('xyz', field('billing'))).toBeNull()
  })

  it('treats -, _, ., space and → as word separators', () => {
    const fields = ['a-locks', 'a_locks', 'a.locks', 'a locks', 'a→locks']
    expect(fields.map(s => matchField('lock', field(s))?.level)).toEqual(fields.map(() => LEVEL_PREFIX))
  })

  it('needs three characters and small gaps for a fuzzy match', () => {
    expect(matchField('bg', field('billing'))).toBeNull()
    expect(matchField('bng', field('billing'))).toBeNull()
    expect(matchField('lng', field('billing'))?.level).toBe(LEVEL_FUZZY)
  })

  it('finds a fuzzy match a greedy scan would miss', () => {
    // Taking the first "b" leaves "c" four characters away; the second "b" works.
    expect(matchField('abc', field('abxbyc'))?.ranges).toEqual([[0, 1], [3, 4], [5, 6]])
  })

  it('returns ranges of the matched characters', () => {
    expect(matchField('blng', field('billing'))?.ranges).toEqual([[0, 1], [2, 3], [5, 7]])
  })
})

describe('search', () => {
  it('returns nothing for an empty query', () => {
    expect(search(input('  '))).toEqual([])
  })

  it('finds a Russian section by its English name and slug', () => {
    const [locks] = search(input('locks'))
    expect(label(locks!)).toBe('section:locks')
    expect(locks!.title).toBe('Блокировки')
    expect(locks!.titleMarks).toEqual([])
    expect(locks!.synonym).toBe('Locks')

    expect(labels('fk-analysis')[0]).toBe('section:fk-analysis')
    expect(labels('блок')[0]).toBe('section:locks')
  })

  it('folds case and ё', () => {
    expect(labels('БЛОК')[0]).toBe('section:locks')
    const index = buildSectionIndex(key => (key === 'Locks' ? 'Всё блокировки' : key))
    expect(labels('все', { sections: index })[0]).toBe('section:locks')
  })

  it('does not pair a single token with sections (D5)', () => {
    expect(labels('billing').filter(l => l.startsWith('cs:'))).toEqual([])
    expect(labels('billing')).toEqual(['cluster:billing-eu', 'cluster:prod-billing'])
  })

  it('pairs a cluster with a section when both get a token', () => {
    const first = search(input('billing repl'))[0]!
    expect(label(first)).toBe('cs:billing-eu:replication')
    expect(first.level).toBe(LEVEL_PREFIX)
    expect(first.title).toBe('billing-eu → Репликация')
    expect(first.titleMarks).toEqual([[0, 7]])
    expect(first.synonym).toBe('Replication')

    const [ru] = search(input('billing репл'))
    expect(ru!.titleMarks).toEqual([[0, 7], [13, 17]])
    expect(ru!.synonym).toBeUndefined()
  })

  it('gives the same results whatever the token order', () => {
    expect(labels('lock billing')).toEqual(labels('billing lock'))
    expect(labels('bill lock')).toEqual(labels('lock bill'))
  })

  it('needs every token to match somewhere', () => {
    expect(labels('billing zzz')).toEqual([])
  })

  it('does not offer Logs for a cluster without log search (US-7)', () => {
    const results = labels('billing logs')
    expect(results).toContain('cs:billing-eu:logs')
    expect(results).not.toContain('cs:prod-billing:logs')
  })

  it('checks section visibility in the current cluster', () => {
    expect(labels('logs', { currentCluster: 'prod-billing' })).not.toContain('section:logs')
    expect(labels('logs', { currentCluster: 'prod-auth' })).toContain('section:logs')
  })

  it('ranks by the weakest token', () => {
    // "auth" is exact and "locks" exact for prod-auth; "bill" is only a prefix.
    const results = labels('auth locks')
    expect(results[0]).toBe('cs:prod-auth:locks')
    expect(labels('bill locks')[0]).toMatch(/^cs:.*billing.*:locks$/)
    expect(search(input('bill locks'))[0]!.level).toBe(LEVEL_PREFIX)
  })

  it('lifts a recent pair over an equal one', () => {
    expect(labels('billing')[0]).toBe('cluster:billing-eu')
    const recent = [{ cluster: 'prod-billing', section: 'main' }]
    expect(labels('billing', { recent })[0]).toBe('cluster:prod-billing')
  })

  it('weighs fresher recent entries higher', () => {
    const recent = [
      { cluster: 'billing-eu', section: 'locks' },
      { cluster: 'prod-billing', section: 'locks' },
    ]
    expect(labels('billing locks', { recent }).slice(0, 2)).toEqual([
      'cs:billing-eu:locks',
      'cs:prod-billing:locks',
    ])
    expect(labels('billing locks', { recent: [...recent].reverse() })[0]).toBe('cs:prod-billing:locks')
  })

  it('weighs a cluster result by the section it lands on', () => {
    const here = { currentSection: 'logs' }
    // prod-billing has no log search, so its row opens the summary
    const recent = [{ cluster: 'prod-billing', section: 'main' }]
    expect(labels('billing', { ...here, recent })[0]).toBe('cluster:prod-billing')
    const stale = [{ cluster: 'prod-billing', section: 'logs' }]
    expect(labels('billing', { ...here, recent: stale })[0]).toBe('cluster:billing-eu')
    const fleet = { currentCluster: null, currentSection: null, recent }
    expect(labels('billing', fleet)[0]).toBe('cluster:prod-billing')
  })

  it('does not let a recent entry beat a better match', () => {
    const recent = [{ cluster: 'prod-billing', section: 'main' }]
    expect(labels('billing-eu', { recent })[0]).toBe('cluster:billing-eu')
  })

  it('finds hosts by display name and address, and databases', () => {
    const cluster: Cluster = {
      name: 'prod-auth',
      instances: [
        { host_name: '10.0.0.11', display_name: 'pg-01' },
        { host_name: '10.0.0.12', display_name: 'pg-02' },
      ],
    }
    const over = { hosts: hostItems(cluster), dbs: ['app', 'db01'] }
    expect(labels('pg-02', over)).toEqual(['host:10.0.0.12'])
    const [byAddress] = search(input('10.0.0.11', over))
    expect(label(byAddress!)).toBe('host:10.0.0.11')
    expect(byAddress!.synonym).toBe('10.0.0.11')
    expect(labels('db01', over)).toEqual(['db:db01'])
  })

  it('orders kinds section, cluster, pair, host, db on equal scores', () => {
    const clusters = [prepareField('locks')]
    const results = labels('locks', {
      clusters,
      visibleIn: () => () => true,
      hosts: [{ value: 'locks', title: 'locks' }],
      dbs: ['locks'],
    })
    expect(results).toEqual(['section:locks', 'cluster:locks', 'host:locks', 'db:locks'])
  })

  it('prefers shorter titles, then alphabetical', () => {
    const clusters = ['billing-long', 'billing-b', 'billing-a'].map(prepareField)
    expect(labels('billing', { clusters })).toEqual([
      'cluster:billing-a',
      'cluster:billing-b',
      'cluster:billing-long',
    ])
  })

  it('returns at most the limit', () => {
    const names = Array.from({ length: 200 }, (_, i) => `pg-${199 - i}`)
    const results = search(input('pg', { clusters: names.map(prepareField) }))
    expect(results).toHaveLength(RESULT_LIMIT)
    const best = [...names].sort((a, b) => a.length - b.length || a.localeCompare(b)).slice(0, RESULT_LIMIT)
    expect(results.map(r => r.cluster)).toEqual(best)
  })

  it('highlights the matched part of the title', () => {
    const [r] = search(input('опис', { sections: ru }))
    expect(r!.title).toBe('Таблицы → Описание таблицы')
    expect(r!.titleMarks).toEqual([[10, 14]])
    expect(r!.synonym).toBeUndefined()
  })

  it('works with the English interface too', () => {
    expect(labels('locks', { sections: en })[0]).toBe('section:locks')
    expect(search(input('locks', { sections: en }))[0]!.titleMarks).toEqual([[0, 5]])
  })

  it('stays fast with 500 clusters', () => {
    const clusters = Array.from({ length: 500 }, (_, i) => prepareField(`prod-cluster-${i}-eu`))
    let calls = 0
    const over = {
      clusters,
      visibleIn: () => {
        calls++
        return () => true
      },
    }
    search(input('a e', over))
    // 16 ms (one frame) is the target; the bound is loose so a busy CI box does not flake.
    const elapsed = ['p', 'prod lo', 'a e', 'clstr'].map(q => {
      const start = performance.now()
      search(input(q, over))
      return performance.now() - start
    })
    expect(Math.max(...elapsed)).toBeLessThan(100)
    // once per cluster, not once per cluster and section
    calls = 0
    search(input('a e', over))
    expect(calls).toBeLessThanOrEqual(clusters.length + 1)
  })

  it('offers no bare sections on Fleet, where there is no cluster to open them in', () => {
    const results = labels('billing locks', { currentCluster: null, currentSection: null })
    expect(results).toContain('cs:prod-billing:locks')
    expect(labels('locks', { currentCluster: null, currentSection: null })).not.toContain('section:locks')
  })
})

describe('current place (§5.5)', () => {
  const currentOf = (q: string, over?: Partial<SearchInput>) =>
    Object.fromEntries(search(input(q, over)).map(r => [label(r), r.current]))

  it('marks the current section, cluster and pair', () => {
    const here = { currentCluster: 'prod-auth', currentSection: 'locks' }
    expect(currentOf('locks', here)['section:locks']).toBe(true)
    expect(currentOf('connections', here)['section:connections']).toBe(false)
    expect(currentOf('auth', here)['cluster:prod-auth']).toBe(true)
    expect(currentOf('billing', here)['cluster:billing-eu']).toBe(false)
    const pairs = currentOf('auth locks', here)
    expect(pairs['cs:prod-auth:locks']).toBe(true)
    expect(currentOf('billing locks', here)['cs:billing-eu:locks']).toBe(false)
  })

  it('marks the current host and database', () => {
    const over = {
      hosts: [
        { value: 'pg-01', title: 'pg-01' },
        { value: 'pg-02', title: 'pg-02' },
      ],
      dbs: ['app', 'app2'],
      currentHost: 'pg-01',
      currentDb: 'app',
    }
    expect(currentOf('pg', over)).toMatchObject({ 'host:pg-01': true, 'host:pg-02': false })
    expect(currentOf('app', over)).toMatchObject({ 'db:app': true, 'db:app2': false })
  })
})

describe('initialIndex', () => {
  it('skips rows that lead to the current place', () => {
    expect(initialIndex([{ current: true }, { current: false }])).toBe(1)
  })

  it('falls back to the first row when every row is current', () => {
    expect(initialIndex([{ current: true }, { current: true }])).toBe(0)
  })

  it('has no selection without rows', () => {
    expect(initialIndex([])).toBe(-1)
  })
})
