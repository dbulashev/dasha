import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/gen/default/default', () => ({}))

import { createI18n } from 'vue-i18n'

import deDE from '@/locales/de_DE.json'
import enUS from '@/locales/en_US.json'
import ruRU from '@/locales/ru_RU.json'
import router from '@/router'
import {
  buildSectionIndex,
  isSectionVisible,
  SECTIONS,
  sectionByRouteName,
  sectionBySlug,
  type VisibilityFacts,
} from '@/utils/palette/sections'
import { translator } from './helpers'

describe('SECTIONS', () => {
  const routes = router.getRoutes()

  it('points every section at a router route with the slug as its first segment', () => {
    expect(SECTIONS.map(s => [s.slug, routes.find(r => r.name === s.routeName)?.path])).toEqual(
      SECTIONS.map(s => [s.slug, `/${s.slug}/:clustername?`]),
    )
  })

  it('covers every cluster page except Fleet', () => {
    const pages = routes
      .filter(r => r.path.endsWith('/:clustername?') && r.name !== 'fleet')
      .map(r => r.name)
      .sort()
    expect(SECTIONS.map(s => s.routeName).sort()).toEqual(pages)
  })

  it('has keys present in every shipped locale', () => {
    const i18n = createI18n({ legacy: false, messages: { ru_RU: ruRU, en_US: enUS, de_DE: deDE } })
    const keys = SECTIONS.flatMap(s => [s.titleKey, s.groupKey ?? []].flat())
    const missing = (['ru_RU', 'en_US', 'de_DE'] as const).flatMap(locale =>
      keys.filter(key => !i18n.global.te(key, locale)).map(key => `${locale}: ${key}`),
    )
    expect(missing).toEqual([])
  })

  it('looks sections up by slug and by route name', () => {
    expect(sectionBySlug('main')?.routeName).toBe('home')
    expect(sectionByRouteName('HealthScore')?.slug).toBe('health-score')
    expect(sectionByRouteName('fleet')).toBeNull()
    expect(sectionByRouteName(undefined)).toBeNull()
  })
})

describe('buildSectionIndex', () => {
  it('titles subsections with their group and indexes the English title and slug', () => {
    const index = buildSectionIndex(translator('ru_RU'))
    const describe = index.find(e => e.section.slug === 'table-describe')!
    expect(describe.title).toBe('Таблицы → Описание таблицы')
    expect(describe.terms.map(t => t.text)).toEqual([
      'Таблицы → Описание таблицы',
      'Tables → Describe table',
      'table-describe',
    ])
  })

  it('drops duplicate terms', () => {
    const index = buildSectionIndex(translator('en_US'))
    const locks = index.find(e => e.section.slug === 'locks')!
    expect(locks.terms.map(t => t.text)).toEqual(['Locks'])
  })
})

describe('isSectionVisible', () => {
  const base: VisibilityFacts = {
    current: true,
    versionNum: null,
    replica: false,
    snapshots: false,
    autosnapshot: false,
    logs: false,
  }
  const activity = sectionBySlug('activity')!
  const io = sectionBySlug('io')!

  it('hides version-gated sections on the current host until the version is known', () => {
    expect(isSectionVisible(activity, base)).toBe(false)
    expect(isSectionVisible(activity, { ...base, versionNum: 140000 })).toBe(true)
    expect(isSectionVisible(io, { ...base, versionNum: 150000 })).toBe(false)
    expect(isSectionVisible(io, { ...base, versionNum: 160000 })).toBe(true)
  })

  it('offers version-gated sections of another cluster unless the cached version is too old', () => {
    const other = { ...base, current: false }
    expect(isSectionVisible(io, other)).toBe(true)
    expect(isSectionVisible(io, { ...other, versionNum: 150000 })).toBe(false)
  })

  it('applies the remaining rules', () => {
    expect(isSectionVisible(sectionBySlug('maintenance')!, { ...base, replica: true })).toBe(false)
    expect(isSectionVisible(sectionBySlug('schema-lint')!, base)).toBe(true)
    expect(isSectionVisible(sectionBySlug('logs')!, base)).toBe(false)
    expect(isSectionVisible(sectionBySlug('log-insights')!, { ...base, logs: true })).toBe(true)
    expect(isSectionVisible(sectionBySlug('query-compare')!, { ...base, snapshots: true })).toBe(true)
    expect(isSectionVisible(sectionBySlug('auto-snapshot')!, base)).toBe(false)
    expect(isSectionVisible(sectionBySlug('locks')!, base)).toBe(true)
  })
})
