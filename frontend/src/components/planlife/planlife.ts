import type {
  PlanLifecycleAnalysis,
  PlanLifecycleIndexChain,
  PlanLifecycleCause,
  PlanLifecyclePath,
  PlanNode,
} from '@/api/models'
import { pathKey } from '@/components/explain/planText'
import { fmtCompactFloat } from '@/utils/format'

type TFunc = (key: string, params?: Record<string, unknown>) => string
type TeFunc = (key: string) => boolean

// pathprobe prints planner node tags (SeqScan); the tree shows EXPLAIN's names.
const TYPE_LABELS: Record<string, string> = {
  SeqScan: 'Seq Scan',
  IndexScan: 'Index Scan',
  IndexOnlyScan: 'Index Only Scan',
  BitmapHeapScan: 'Bitmap Heap Scan',
  BitmapAnd: 'BitmapAnd',
  BitmapOr: 'BitmapOr',
  SampleScan: 'Sample Scan',
  TidScan: 'Tid Scan',
  TidRangeScan: 'Tid Range Scan',
  NestLoop: 'Nested Loop',
  HashJoin: 'Hash Join',
  MergeJoin: 'Merge Join',
  MergeAppend: 'Merge Append',
  IncrementalSort: 'Incremental Sort',
  GatherMerge: 'Gather Merge',
  SubqueryScan: 'Subquery Scan',
  FunctionScan: 'Function Scan',
  ValuesScan: 'Values Scan',
  CteScan: 'CTE Scan',
  WorkTableScan: 'WorkTable Scan',
  ForeignScan: 'Foreign Scan',
  CustomScan: 'Custom Scan',
  ProjectSet: 'ProjectSet',
  LockRows: 'LockRows',
  ModifyTable: 'ModifyTable',
  Agg: 'Aggregate',
  WindowAgg: 'WindowAgg',
  RecursiveUnion: 'Recursive Union',
}

export function pathTypeLabel(pathtype: string): string {
  return TYPE_LABELS[pathtype] ?? pathtype
}

// Small costs keep two decimals: startup costs such as 0.71 vs 0.42 decide add_path().
export function fmtCost(v: number | null | undefined): string {
  if (v == null) return '—'
  return Math.abs(v) >= 1000 ? fmtCompactFloat(v) : v.toFixed(2)
}

export function fmtCostRange(startup: number | null | undefined, total: number | null | undefined): string {
  return startup != null ? `${fmtCost(startup)}..${fmtCost(total)}` : fmtCost(total)
}

export function fmtRatio(r: number): string {
  return r < 10 ? `×${r.toFixed(1)}` : `×${fmtCompactFloat(r)}`
}

// A term pathprobe writes as is (an event kind, a skip reason) shown in the UI language when known.
export function termText(group: string, value: string, t: TFunc, te: TeFunc): string {
  const key = `planLifecycle.${group}.${value.replace(/\W/g, '_')}`
  return te(key) ? t(key) : value
}

export function pathsById(a: PlanLifecycleAnalysis): Map<number, PlanLifecyclePath> {
  return new Map(a.paths.map(p => [p.path_id, p]))
}

export function relAliases(a: PlanLifecycleAnalysis): Map<number, string> {
  return new Map(a.rels.map(r => [r.index, r.alias]))
}

// "{1,2}" -> "c ⋈ o"; "?" -> upper
export function relText(rel: string, aliases: Map<number, string>, t: TFunc): string {
  if (rel === '?') return t('planLifecycle.upperRel')
  const ids = rel.replace(/[{}]/g, '').split(',').filter(Boolean).map(Number)
  const names = ids.map(i => aliases.get(i))
  return names.every(Boolean) ? names.join(' ⋈ ') : rel
}

// rels below a SubqueryScan are numbered in the subquery: no aliases for them
export function paramText(p: PlanLifecyclePath, aliases: Map<number, string>): string {
  if (!p.required_outer?.length) return ''
  return p.required_outer.map(i => (p.in_subquery ? undefined : aliases.get(i)) ?? String(i)).join(', ')
}

function baseAlias(p: PlanLifecyclePath, aliases: Map<number, string>): string | undefined {
  if (p.rel === '?' || p.rel.includes(',') || p.in_subquery) return undefined
  return aliases.get(Number(p.rel.replace(/[{}]/g, '')))
}

// "Index Only Scan c (pp_customers_pkey)", "Nested Loop c ⋈ o"; join rels only with withRel.
export function pathName(
  p: PlanLifecyclePath,
  aliases: Map<number, string>,
  t: TFunc,
  withRel = false,
): string {
  const parts = [pathTypeLabel(p.pathtype)]
  const base = baseAlias(p, aliases)
  if (base) parts.push(base)
  else if (withRel && !p.in_subquery) parts.push(relText(p.rel, aliases, t))
  if (p.index_name) parts.push(`(${p.index_name})`)
  return parts.join(' ')
}

// A summary sentence: a locale key whose {path} placeholder is a link to pathId.
export interface Verdict {
  key: string
  pathId?: number
  pathText?: string
  params: Record<string, string>
}

export function finalVerdict(a: PlanLifecycleAnalysis, aliases: Map<number, string>, t: TFunc): Verdict | null {
  const f = a.final.path_id != null ? pathsById(a).get(a.final.path_id) : undefined
  if (!f) return null
  return {
    key: 'planLifecycle.summary.final',
    pathId: f.path_id,
    pathText: `${pathName(f, aliases, t, true)} #${f.path_id}`,
    params: { cost: fmtCost(f.cost_total) },
  }
}

// Why an index is not in the plan, from the cheapest path built over it; null when it is in the plan.
export function indexVerdict(
  ix: PlanLifecycleIndexChain,
  a: PlanLifecycleAnalysis,
  aliases: Map<number, string>,
  t: TFunc,
): Verdict | null {
  if (ix.outcome === 'in_plan') return null
  const byId = pathsById(a)
  const best = ix.best_path_id != null ? byId.get(ix.best_path_id) : undefined
  if (!best) return { key: `planLifecycle.outcome.${ix.outcome}`, params: {} }
  const link = { pathId: best.path_id, pathText: `${pathName(best, aliases, t, true)} #${best.path_id}` }
  const f = a.final.path_id != null ? byId.get(a.final.path_id) : undefined
  if (ix.best_at_final_level && f?.cost_total && best.cost_total != null) {
    return {
      key: 'planLifecycle.summary.lostAtTop',
      ...link,
      params: { cost: fmtCost(best.cost_total), ratio: fmtRatio(best.cost_total / f.cost_total) },
    }
  }
  return {
    key: 'planLifecycle.summary.stoppedAt',
    ...link,
    params: { rel: relText(best.rel, aliases, t), cost: fmtCost(best.cost_total) },
  }
}

export function childIds(p: PlanLifecyclePath): number[] {
  const out = [p.outer_path_id, p.inner_path_id, p.subpath_id].filter((id): id is number => id != null)
  return [...out, ...(p.child_path_ids ?? [])]
}

const BITMAP_PARENTS = new Set(['BitmapHeapScan', 'BitmapAnd', 'BitmapOr'])

export interface PathTree {
  root: PlanNode | null
  keyByPath: Map<number, string>
  pathByKey: Map<string, number>
}

// The tree of the final path in PlanTree's node shape. A path without events has
// no costs of its own; the tree shows 0 for them and the panel says why.
export function finalTree(a: PlanLifecycleAnalysis): PathTree {
  const byId = pathsById(a)
  const aliases = relAliases(a)
  const keyByPath = new Map<number, string>()
  const pathByKey = new Map<string, number>()

  function build(id: number, path: number[], parent?: PlanLifecyclePath): PlanNode | null {
    const p = byId.get(id)
    if (!p || keyByPath.has(id)) return null

    const key = pathKey(path)
    keyByPath.set(id, key)
    pathByKey.set(key, id)

    const children: PlanNode[] = []
    for (const child of childIds(p)) {
      const node = build(child, [...path, children.length], p)
      if (node) children.push(node)
    }

    // an index path under a BitmapHeapScan, BitmapAnd or BitmapOr is a Bitmap Index Scan in EXPLAIN
    const isBitmapQual = p.pathtype === 'IndexScan' && parent != null && BITMAP_PARENTS.has(parent.pathtype)
    const base = baseAlias(p, aliases)

    return {
      type: isBitmapQual ? 'Bitmap Index Scan' : pathTypeLabel(p.pathtype),
      relation: isBitmapQual ? undefined : base,
      index_name: p.index_name ?? undefined,
      parallel: false,
      startup_cost: p.cost_startup ?? 0,
      total_cost: p.cost_total ?? 0,
      plan_rows: p.rows ?? 0,
      plan_width: 0,
      children,
    }
  }

  const id = a.final.path_id
  const root = id != null ? build(id, []) : null
  return { root, keyByPath, pathByKey }
}

export function matched(a: PlanLifecycleAnalysis): boolean {
  return !!a.plan && (a.match?.status === 'match' || a.match?.status === 'match_with_notes')
}

// The uploaded plan as the tree when it matches the final path: its nodes carry the
// actual rows and the plan rules; paths are bound to them by plan_node.
export function planTree(a: PlanLifecycleAnalysis): PathTree {
  if (!a.plan || !matched(a)) return finalTree(a)

  const keyByPath = new Map<number, string>()
  const pathByKey = new Map<string, number>()
  for (const p of a.paths) {
    if (p.plan_node == null) continue
    const key = pathKey(p.plan_node)
    keyByPath.set(p.path_id, key)
    pathByKey.set(key, p.path_id)
  }

  return { root: a.plan.root, keyByPath, pathByKey }
}

export function planNodeAt(root: PlanNode | undefined, path: number[] | undefined): PlanNode | undefined {
  if (!root || !path) return undefined
  let n: PlanNode | undefined = root
  for (const i of path) n = n?.children?.[i]
  return n
}

export function fateColor(fate: string): string {
  switch (fate) {
    case 'chosen':
      return 'success'
    case 'survived':
      return 'info'
    case 'displaced':
    case 'removed':
      return 'warning'
    case 'rejected':
      return 'error'
    default:
      return 'grey'
  }
}

export function causeText(c: PlanLifecycleCause | undefined, t: TFunc): string {
  if (!c) return ''
  return t(`planLifecycle.cause.${c.code}`, {
    gap: fmtCost(c.gap == null ? undefined : Math.abs(c.gap)),
    startup: fmtCost(c.startup),
    winnerStartup: fmtCost(c.winner_startup),
  })
}

// Paths of one relation, the chosen and surviving ones first, then by cost.
const FATE_ORDER: Record<string, number> = {
  chosen: 0,
  survived: 1,
  displaced: 2,
  removed: 3,
  rejected: 4,
  child_only: 5,
  unknown: 6,
}

export function sortedCounts(counts: Record<string, number>): [string, number][] {
  return Object.entries(counts).sort(([x], [y]) => (FATE_ORDER[x] ?? 9) - (FATE_ORDER[y] ?? 9))
}

export function sortPaths(paths: PlanLifecyclePath[]): PlanLifecyclePath[] {
  return [...paths].sort(
    (x, y) =>
      (FATE_ORDER[x.fate] ?? 9) - (FATE_ORDER[y.fate] ?? 9) ||
      (x.cost_total ?? Infinity) - (y.cost_total ?? Infinity),
  )
}
