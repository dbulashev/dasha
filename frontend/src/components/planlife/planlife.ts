import type {
  PlanLifecycleAnalysis,
  PlanLifecycleCause,
  PlanLifecyclePath,
  PlanNode,
} from '@/api/models'
import { pathKey } from '@/components/explain/planText'

type TFunc = (key: string, params?: Record<string, unknown>) => string

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
    const single = p.rel !== '?' && !p.rel.includes(',') && !p.in_subquery
    const base = single ? aliases.get(Number(p.rel.replace(/[{}]/g, ''))) : undefined

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

function num(v: number | undefined): string {
  return v == null ? '—' : v.toFixed(2)
}

export function causeText(c: PlanLifecycleCause | undefined, t: TFunc): string {
  if (!c) return ''
  return t(`planLifecycle.cause.${c.code}`, {
    gap: num(c.gap == null ? undefined : Math.abs(c.gap)),
    startup: num(c.startup),
    winnerStartup: num(c.winner_startup),
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

export function sortPaths(paths: PlanLifecyclePath[]): PlanLifecyclePath[] {
  return [...paths].sort(
    (x, y) =>
      (FATE_ORDER[x.fate] ?? 9) - (FATE_ORDER[y.fate] ?? 9) ||
      (x.cost_total ?? Infinity) - (y.cost_total ?? Infinity),
  )
}
