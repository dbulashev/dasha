import { ACTIVITY_MIN_VERSION_NUM } from '@/components/activity/classes'
import { IO_MIN_VERSION_NUM } from '@/components/io/types'
import { prepareField, type PreparedField } from './search'

export type VisibilityRule =
  | 'always'
  | 'activity'
  | 'io'
  | 'snapshots'
  | 'autosnapshot'
  | 'logs'
  | 'primaryOnly'

export interface SectionDef {
  /** first path segment: 'main', 'health-score' */
  slug: string
  /** differs from the slug for 'main' (home) and 'health-score' (HealthScore) */
  routeName: string
  titleKey: string
  groupKey?: string
  icon: string
  rule: VisibilityRule
  /** query parameters the menu link carries over besides host and db */
  keepQuery?: string[]
}

const QUERIES = { groupKey: 'Active Queries', icon: 'mdi-database-clock-outline' }
const TABLES = { groupKey: 'Tables', icon: 'mdi-table' }
const INDEXES = { groupKey: 'Indexes', icon: 'mdi-family-tree' }
const LOGS = { groupKey: 'Logs', icon: 'mdi-text-box-search-outline' }

// Menu order of App.vue. Fleet is not in the menu, so it is not here either.
export const SECTIONS: readonly SectionDef[] = [
  { slug: 'main', routeName: 'home', titleKey: 'Home', icon: 'mdi-sigma', rule: 'always' },
  { slug: 'health-score', routeName: 'HealthScore', titleKey: 'healthScore.page.menuItem', icon: 'mdi-heart-pulse', rule: 'always' },
  { slug: 'connections', routeName: 'connections', titleKey: 'Connections', icon: 'mdi-connection', rule: 'always' },
  { slug: 'queries', routeName: 'queries', titleKey: 'activity.menuCurrent', ...QUERIES, rule: 'always' },
  { slug: 'activity', routeName: 'activity', titleKey: 'activity.menu', ...QUERIES, rule: 'activity' },
  { slug: 'query-stats', routeName: 'query-stats', titleKey: 'Query Stats', icon: 'mdi-chart-bar', rule: 'always' },
  { slug: 'query-report', routeName: 'query-report', titleKey: 'Query Report', icon: 'mdi-file-chart-outline', rule: 'always' },
  { slug: 'query-compare', routeName: 'query-compare', titleKey: 'Query Compare', icon: 'mdi-compare', rule: 'snapshots' },
  { slug: 'tables', routeName: 'tables', titleKey: 'tables.menuOverview', ...TABLES, rule: 'always' },
  { slug: 'table-describe', routeName: 'table-describe', titleKey: 'tables.menuDescribe', ...TABLES, rule: 'always', keepQuery: ['schema', 'table'] },
  { slug: 'indexes', routeName: 'indexes', titleKey: 'indexes.menuOverview', ...INDEXES, rule: 'always' },
  { slug: 'indexes-usage', routeName: 'indexes-usage', titleKey: 'indexes.menuUsage', ...INDEXES, rule: 'always' },
  { slug: 'indexes-problems', routeName: 'indexes-problems', titleKey: 'indexes.menuProblems', ...INDEXES, rule: 'always' },
  { slug: 'indexes-analysis', routeName: 'indexes-analysis', titleKey: 'indexes.menuAnalysis', ...INDEXES, rule: 'always' },
  { slug: 'locks', routeName: 'locks', titleKey: 'Locks', icon: 'mdi-lock-outline', rule: 'always' },
  { slug: 'progress', routeName: 'progress', titleKey: 'Operation progress', icon: 'mdi-progress-question', rule: 'always' },
  { slug: 'fk-analysis', routeName: 'fk-analysis', titleKey: 'FK Analysis', icon: 'mdi-relation-many-to-many', rule: 'always' },
  { slug: 'replication', routeName: 'replication', titleKey: 'Replication', icon: 'mdi-database-sync-outline', rule: 'always' },
  { slug: 'io', routeName: 'io', titleKey: 'io.menu', icon: 'mdi-harddisk', rule: 'io' },
  { slug: 'maintenance', routeName: 'maintenance', titleKey: 'Maintenance', icon: 'mdi-wrench-outline', rule: 'primaryOnly' },
  { slug: 'schema-lint', routeName: 'schema-lint', titleKey: 'schemaLint.page.menuItem', icon: 'mdi-clipboard-check-outline', rule: 'primaryOnly' },
  { slug: 'settings', routeName: 'settings', titleKey: 'Settings', icon: 'mdi-database-settings-outline', rule: 'always' },
  { slug: 'logs', routeName: 'logs', titleKey: 'logs.menuSearch', ...LOGS, rule: 'logs' },
  { slug: 'log-insights', routeName: 'log-insights', titleKey: 'logs.menuInsights', ...LOGS, rule: 'logs' },
  { slug: 'auto-snapshot', routeName: 'auto-snapshot', titleKey: 'autosnapshot.menu', icon: 'mdi-camera-timer', rule: 'autosnapshot' },
]

export const MAIN_SECTION: SectionDef = SECTIONS[0]!

const bySlug = new Map(SECTIONS.map(s => [s.slug, s]))
const byRouteName = new Map(SECTIONS.map(s => [s.routeName, s]))

export function sectionBySlug(slug: string): SectionDef | null {
  return bySlug.get(slug) ?? null
}

/** The section of a route; null on pages outside the menu (Fleet). */
export function sectionByRouteName(name: unknown): SectionDef | null {
  return typeof name === 'string' ? (byRouteName.get(name) ?? null) : null
}

// What the visibility rules need to know about the target. Gathered from the
// stores by useSectionVisibility; kept separate so the rules stay pure.
export interface VisibilityFacts {
  /** the place the user is on now; only then are unknown facts treated as "hidden" */
  current: boolean
  /** null while the server version is not known */
  versionNum: number | null
  replica: boolean
  snapshots: boolean
  autosnapshot: boolean
  logs: boolean
}

export function isSectionVisible(section: SectionDef, facts: VisibilityFacts): boolean {
  switch (section.rule) {
    case 'always':
      return true
    case 'activity':
      return versionAtLeast(facts, ACTIVITY_MIN_VERSION_NUM)
    case 'io':
      return versionAtLeast(facts, IO_MIN_VERSION_NUM)
    case 'snapshots':
      return facts.snapshots
    case 'autosnapshot':
      return facts.autosnapshot
    case 'logs':
      return facts.logs
    case 'primaryOnly':
      return !facts.replica
  }
}

// The menu hides the item until the version is known. For another cluster the
// palette has no version unless it happens to be cached, and the page itself
// explains an old server, so the item is offered there.
function versionAtLeast(facts: VisibilityFacts, min: number): boolean {
  if (!facts.current && facts.versionNum === null) return true
  return (facts.versionNum ?? 0) >= min
}

export interface SectionIndexEntry {
  section: SectionDef
  /** in the interface language; subsections carry the group: "Tables → Describe table" */
  title: string
  /** terms[0] is the title; then the English title and the slug, deduplicated */
  terms: PreparedField[]
}

export type Translate = (key: string, locale?: string) => string

export const INDEX_EXTRA_LOCALE = 'en_US'

export function sectionTitle(section: SectionDef, t: Translate, locale?: string): string {
  const title = t(section.titleKey, locale)
  return section.groupKey ? `${t(section.groupKey, locale)} → ${title}` : title
}

export function buildSectionIndex(t: Translate): SectionIndexEntry[] {
  return SECTIONS.map(section => {
    const title = sectionTitle(section, t)
    const texts = [title, sectionTitle(section, t, INDEX_EXTRA_LOCALE), section.slug]
    const unique = texts.filter((s, i) => texts.findIndex(o => o.toLowerCase() === s.toLowerCase()) === i)
    return { section, title, terms: unique.map(prepareField) }
  })
}
