import type { HostItem } from '@/utils/clusters'
import { MAIN_SECTION, type SectionDef, type SectionIndexEntry } from './sections'

export type MatchLevel = 0 | 1 | 2 | 3

export const LEVEL_EXACT = 3
export const LEVEL_PREFIX = 2
export const LEVEL_SUBSTRING = 1
export const LEVEL_FUZZY = 0

export const FUZZY_MIN_LENGTH = 3
const FUZZY_MAX_GAP = 2

export const RESULT_LIMIT = 50

export type Range = [start: number, end: number]

// Lowercased character by character so that offsets in the normalized string are
// offsets in the original: the highlight ranges are applied to the original text.
export function normalize(s: string): string {
  let out = ''
  for (const ch of s) {
    const lower = ch.toLowerCase()
    out += lower.length === ch.length ? lower : ch
  }
  return out.replaceAll('ё', 'е')
}

const QUERY_SEPARATORS = /[\s→]+/

export function tokenize(query: string): string[] {
  return normalize(query).split(QUERY_SEPARATORS).filter(Boolean)
}

const WORD_SEPARATORS = new Set(['-', '_', '.', ' ', '→'])

export interface PreparedField {
  text: string
  norm: string
  /** [start, end) of each word of norm */
  words: Range[]
}

export function prepareField(text: string): PreparedField {
  const norm = normalize(text)
  const words: Range[] = []
  let start = -1
  for (let i = 0; i <= norm.length; i++) {
    const sep = i === norm.length || WORD_SEPARATORS.has(norm[i]!)
    if (sep && start >= 0) {
      words.push([start, i])
      start = -1
    } else if (!sep && start < 0) {
      start = i
    }
  }
  return { text, norm, words }
}

export interface PreparedHost {
  host: HostItem
  /** the title, then the address when it differs */
  terms: PreparedField[]
}

export function prepareHost(host: HostItem): PreparedHost {
  return { host, terms: [prepareField(host.title), ...(host.address ? [prepareField(host.address)] : [])] }
}

export interface PreparedClusterHost extends PreparedHost {
  cluster: string
}

export interface FieldMatch {
  level: MatchLevel
  ranges: Range[]
}

export function matchField(token: string, field: PreparedField): FieldMatch | null {
  const { norm, words } = field
  if (!token || token.length > norm.length) return null

  if (norm === token) return { level: LEVEL_EXACT, ranges: [[0, norm.length]] }
  const word = words.find(([s, e]) => e - s === token.length && norm.startsWith(token, s))
  if (word) return { level: LEVEL_EXACT, ranges: [[word[0], word[1]]] }

  const prefix = norm.startsWith(token) ? 0 : words.find(([s]) => norm.startsWith(token, s))?.[0]
  if (prefix !== undefined) return { level: LEVEL_PREFIX, ranges: [[prefix, prefix + token.length]] }

  const at = norm.indexOf(token)
  if (at >= 0) return { level: LEVEL_SUBSTRING, ranges: [[at, at + token.length]] }

  const fuzzy = fuzzyRanges(token, norm)
  return fuzzy ? { level: LEVEL_FUZZY, ranges: fuzzy } : null
}

// Characters of the token in order, at most FUZZY_MAX_GAP characters skipped
// between neighbours. A greedy scan can miss a match (taking the earliest
// occurrence may push the next character out of reach), so this keeps, for
// every position, the previous position it was reached from.
function fuzzyRanges(token: string, norm: string): Range[] | null {
  if (token.length < FUZZY_MIN_LENGTH) return null
  let reached: number[] = []
  const from: Map<number, number>[] = []
  for (let i = 0; i < norm.length; i++) if (norm[i] === token[0]) reached.push(i)
  for (let k = 1; k < token.length && reached.length; k++) {
    const next: number[] = []
    const back = new Map<number, number>()
    for (const p of reached) {
      for (let j = p + 1; j <= p + 1 + FUZZY_MAX_GAP && j < norm.length; j++) {
        if (norm[j] === token[k] && !back.has(j)) {
          back.set(j, p)
          next.push(j)
        }
      }
    }
    next.sort((a, b) => a - b)
    from.push(back)
    reached = next
  }
  if (!reached.length) return null

  const positions = [reached[0]!]
  for (let k = from.length - 1; k >= 0; k--) positions.unshift(from[k]!.get(positions[0]!)!)
  return mergeRanges(positions.map(p => [p, p + 1]))
}

export function mergeRanges(ranges: Range[]): Range[] {
  const sorted = [...ranges].sort((a, b) => a[0] - b[0])
  const out: Range[] = []
  for (const r of sorted) {
    const last = out[out.length - 1]
    if (last && r[0] <= last[1]) last[1] = Math.max(last[1], r[1])
    else out.push([r[0], r[1]])
  }
  return out
}

interface TermsMatch extends FieldMatch {
  term: number
}

// Best level over the terms; on a tie the earlier term wins, so the shown title
// (term 0) is preferred and gets the highlight.
function matchTerms(token: string, terms: PreparedField[]): TermsMatch | null {
  let best: TermsMatch | null = null
  for (let i = 0; i < terms.length; i++) {
    const m = matchField(token, terms[i]!)
    if (m && (!best || m.level > best.level)) best = { ...m, term: i }
  }
  return best
}

export type SearchKind = 'section' | 'cluster' | 'cluster-section' | 'host' | 'cluster-host' | 'db'

const KIND_ORDER: Record<SearchKind, number> = {
  section: 0,
  cluster: 1,
  'cluster-section': 2,
  host: 3,
  'cluster-host': 4,
  db: 5,
}

export interface SearchResult {
  kind: SearchKind
  /** for 'cluster', 'cluster-section' and 'cluster-host' */
  cluster?: string
  /** for 'section' and 'cluster-section' */
  section?: SectionDef
  host?: HostItem
  db?: string
  title: string
  titleMarks: Range[]
  /** the matched text when it is not the title: an English name or a slug */
  synonym?: string
  level: MatchLevel
  /** leads where the user already is (§5.5) */
  current: boolean
}

export interface RecentPair {
  cluster: string
  section: string
}

export interface SearchInput {
  query: string
  /** prepared once per cluster list, see prepareField */
  clusters: PreparedField[]
  sections: SectionIndexEntry[]
  /** hosts and databases of the current cluster, prepared once per opening */
  hosts: PreparedHost[]
  dbs: PreparedField[]
  /** hosts of every cluster but the current one; fuzzy matches are dropped there */
  foreignHosts: PreparedClusterHost[]
  /** null on Fleet: there are no section results then, only clusters to go to */
  currentCluster: string | null
  /** slug of the current section; a cluster result leads there */
  currentSection: string | null
  currentHost: string | null
  currentDb: string | null
  /** called once per cluster; the predicate is asked about every section of it */
  visibleIn: (cluster: string) => (section: SectionDef) => boolean
  /** newest first */
  recent: RecentPair[]
}

interface Ranked extends Omit<SearchResult, 'titleMarks' | 'synonym'> {
  recentWeight: number
  levelSum: number
  // Highlights are worked out only for the rows that make the cut: a two-letter
  // query can pair thousands of cluster and section combinations.
  describe: () => Pick<SearchResult, 'titleMarks' | 'synonym'>
}

type TokenMatches = (TermsMatch | null)[]

export function search(input: SearchInput): SearchResult[] {
  const tokens = tokenize(input.query)
  if (!tokens.length) return []

  const recentWeight = recentWeights(input.recent)
  const weight = (cluster: string | null, section: string | null) =>
    cluster && section ? (recentWeight.get(pairKey(cluster, section)) ?? 0) : 0

  const out: Ranked[] = []

  const sectionMatches = input.sections.map(entry => tokens.map(tok => matchTerms(tok, entry.terms)))
  const clusterMatches = input.clusters.map(field => tokens.map(tok => matchTerms(tok, [field])))

  const { currentCluster, currentSection } = input
  const visibles = new Map<string, (section: SectionDef) => boolean>()
  const visibleIn = (cluster: string) => {
    let v = visibles.get(cluster)
    if (!v) visibles.set(cluster, (v = input.visibleIn(cluster)))
    return v
  }
  const visibleHere = currentCluster ? visibleIn(currentCluster) : null
  const currentDef = input.sections.find(e => e.section.slug === currentSection)?.section ?? null
  // A cluster result opens the current section, or the summary where that is
  // hidden or there is none (Fleet): the recent entry is weighed by where it lands.
  const landsOn = (cluster: string) =>
    currentDef && visibleIn(cluster)(currentDef) ? currentDef.slug : MAIN_SECTION.slug
  input.sections.forEach((entry, si) => {
    const ms = sectionMatches[si]!
    if (!all(ms) || !visibleHere?.(entry.section)) return
    out.push({
      kind: 'section',
      section: entry.section,
      title: entry.title,
      current: entry.section.slug === currentSection,
      describe: () => describeSection(entry, ms),
      ...score(ms),
      recentWeight: weight(input.currentCluster, entry.section.slug),
    })
  })

  input.clusters.forEach((field, ci) => {
    const mc = clusterMatches[ci]!
    if (!all(mc)) return
    out.push({
      kind: 'cluster',
      cluster: field.text,
      title: field.text,
      current: field.text === currentCluster,
      describe: () => ({ titleMarks: marksOf(mc) }),
      ...score(mc),
      recentWeight: weight(field.text, landsOn(field.text)),
    })
  })

  if (tokens.length >= 2) {
    const someSections = input.sections.map((_, si) => si).filter(si => sectionMatches[si]!.some(Boolean))
    input.clusters.forEach((field, ci) => {
      const mc = clusterMatches[ci]!
      if (!mc.some(Boolean)) return
      const visible = visibleIn(field.text)
      for (const si of someSections) {
        const ms = sectionMatches[si]!
        const split = splitTokens(mc, ms)
        if (!split) continue
        const entry = input.sections[si]!
        if (!visible(entry.section)) continue
        out.push({
          kind: 'cluster-section',
          cluster: field.text,
          section: entry.section,
          title: field.text + PAIR_SEPARATOR + entry.title,
          current: field.text === currentCluster && entry.section.slug === currentSection,
          describe: () => {
            const offset = field.text.length + PAIR_SEPARATOR.length
            const section = describeSection(entry, split.section)
            return {
              titleMarks: [
                ...marksOf(split.cluster),
                ...section.titleMarks.map(([s, e]): Range => [s + offset, e + offset]),
              ],
              ...(section.synonym ? { synonym: section.synonym } : {}),
            }
          },
          ...score(tokens.map((_, i) => split.cluster[i] ?? split.section[i] ?? null)),
          recentWeight: weight(field.text, entry.section.slug),
        })
      }
    })
  }

  for (const { host, terms } of input.hosts) {
    const mh = tokens.map(tok => matchTerms(tok, terms))
    if (!all(mh)) continue
    const synonymTerm = mh.find(m => m.term !== 0)
    out.push({
      kind: 'host',
      host,
      title: host.title,
      current: host.value === input.currentHost,
      describe: () => ({
        titleMarks: marksOf(mh),
        ...(synonymTerm ? { synonym: terms[synonymTerm.term]!.text } : {}),
      }),
      ...score(mh),
      recentWeight: 0,
    })
  }

  // Fuzzy matching over the hosts of the whole fleet finds half of it.
  for (const { cluster, host, terms } of input.foreignHosts) {
    const mh = tokens.map(tok => matchTerms(tok, terms))
    if (!all(mh) || mh.some(m => m.level === LEVEL_FUZZY)) continue
    const synonymTerm = mh.find(m => m.term !== 0)
    out.push({
      kind: 'cluster-host',
      cluster,
      host,
      title: host.title,
      current: false,
      describe: () => ({
        titleMarks: marksOf(mh),
        ...(synonymTerm ? { synonym: terms[synonymTerm.term]!.text } : {}),
      }),
      ...score(mh),
      recentWeight: 0,
    })
  }

  for (const field of input.dbs) {
    const db = field.text
    const md = tokens.map(tok => matchTerms(tok, [field]))
    if (!all(md)) continue
    out.push({
      kind: 'db',
      db,
      title: db,
      current: db === input.currentDb,
      describe: () => ({ titleMarks: marksOf(md) }),
      ...score(md),
      recentWeight: 0,
    })
  }

  return topRanked(out, RESULT_LIMIT).map(finish)
}

export const PAIR_SEPARATOR = ' → '

function all(ms: TokenMatches): ms is TermsMatch[] {
  return ms.every(Boolean)
}

function score(ms: TermsMatch[] | TokenMatches): { level: MatchLevel; levelSum: number } {
  let level: MatchLevel = LEVEL_EXACT
  let levelSum = 0
  for (const m of ms) {
    if (!m) continue
    if (m.level < level) level = m.level
    levelSum += m.level
  }
  return { level, levelSum }
}

// Highlights come from term 0 only: it is the text that is shown.
function marksOf(ms: TokenMatches): Range[] {
  return mergeRanges(ms.flatMap(m => (m && m.term === 0 ? m.ranges : [])))
}

function describeSection(entry: SectionIndexEntry, ms: TokenMatches) {
  const synonymTerm = ms.find(m => m && m.term !== 0)
  return {
    titleMarks: marksOf(ms),
    ...(synonymTerm ? { synonym: entry.terms[synonymTerm.term]!.text } : {}),
  }
}

interface Split {
  cluster: TokenMatches
  section: TokenMatches
}

// Assigns every token to the cluster or to the section so that each side gets
// at least one token (D5), keeping the weakest token as strong as possible.
// Each token goes to its better side; if that leaves one side empty, the one
// token that costs least is moved over.
function splitTokens(mc: TokenMatches, ms: TokenMatches): Split | null {
  const n = mc.length
  const toCluster: boolean[] = []
  for (let i = 0; i < n; i++) {
    const c = mc[i]
    const s = ms[i]
    if (!c && !s) return null
    toCluster.push(!!c && (!s || c.level >= s.level))
  }

  const clusterCount = toCluster.filter(Boolean).length
  if (clusterCount === 0 || clusterCount === n) {
    const moveToCluster = clusterCount === 0
    const here = moveToCluster ? ms : mc
    const other = moveToCluster ? mc : ms
    let best = -1
    let bestMin = -1
    let bestSum = -1
    for (let j = 0; j < n; j++) {
      const moved = other[j]
      if (!moved) continue
      let min: number = moved.level
      let sum: number = moved.level
      for (let i = 0; i < n; i++) {
        if (i === j) continue
        min = Math.min(min, here[i]!.level)
        sum += here[i]!.level
      }
      if (min > bestMin || (min === bestMin && sum > bestSum)) {
        best = j
        bestMin = min
        bestSum = sum
      }
    }
    if (best < 0) return null
    toCluster[best] = moveToCluster
  }

  return {
    cluster: mc.map((m, i) => (toCluster[i] ? m : null)),
    section: ms.map((m, i) => (toCluster[i] ? null : m)),
  }
}

function pairKey(cluster: string, section: string): string {
  return `${cluster}\u0000${section}`
}

function recentWeights(recent: RecentPair[]): Map<string, number> {
  const out = new Map<string, number>()
  recent.forEach((r, i) => {
    const k = pairKey(r.cluster, r.section)
    if (!out.has(k)) out.set(k, Math.max(10 - i, 0))
  })
  return out
}

const collator = new Intl.Collator()

function compareRanked(a: Ranked, b: Ranked): number {
  return (
    b.level - a.level ||
    b.recentWeight - a.recentWeight ||
    b.levelSum - a.levelSum ||
    KIND_ORDER[a.kind] - KIND_ORDER[b.kind] ||
    a.title.length - b.title.length ||
    collator.compare(a.title, b.title)
  )
}

// Most candidates of a broad query lose to the current last row, so keeping a
// short sorted list beats sorting thousands of pairs (measured: 6 ms vs < 1 ms).
function topRanked(items: Ranked[], limit: number): Ranked[] {
  const top: Ranked[] = []
  for (const item of items) {
    if (top.length === limit && compareRanked(item, top[limit - 1]!) >= 0) continue
    let i = top.length
    while (i > 0 && compareRanked(item, top[i - 1]!) < 0) i--
    top.splice(i, 0, item)
    if (top.length > limit) top.pop()
  }
  return top
}

function finish({ recentWeight: _w, levelSum: _s, describe, ...result }: Ranked): SearchResult {
  return { ...result, ...describe() }
}

/** The row selected when results appear: the first one that leads somewhere new. */
export function initialIndex(items: readonly { current: boolean }[]): number {
  if (!items.length) return -1
  const i = items.findIndex(item => !item.current)
  return i >= 0 ? i : 0
}
