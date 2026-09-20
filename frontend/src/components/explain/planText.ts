import type { PlanFinding, PlanNode } from '@/api/models'
import { fmtBytes, fmtCompact, fmtCompactFloat, fmtPct } from '@/utils/format'

export interface FlatPlanNode {
  key: string
  path: number[]
  depth: number
  node: PlanNode
}

export function pathKey(path: number[] | undefined): string {
  return (path ?? []).join('.')
}

export function flattenPlan(root: PlanNode): FlatPlanNode[] {
  const out: FlatPlanNode[] = []

  function walk(node: PlanNode, path: number[], depth: number) {
    out.push({ key: pathKey(path), path, depth, node })
    ;(node.children ?? []).forEach((child, i) => walk(child, [...path, i], depth + 1))
  }

  walk(root, [], 0)
  return out
}

// Modifier values PostgreSQL folds into the node name it prints.
const IMPLIED_MODIFIERS = new Set(['Inner', 'Forward', 'Plain', 'Simple'])

export function nodeLabel(node: PlanNode): string {
  const name = [node.partial_mode, node.parallel ? 'Parallel' : '', node.type]
    .filter(Boolean)
    .join(' ')
  const mods = [node.strategy, node.join_type, node.scan_direction, node.operation].filter(
    (m): m is string => !!m && !IMPLIED_MODIFIERS.has(m),
  )
  return mods.length ? `${name} (${mods.join(', ')})` : name
}

export function nodeRelation(node: PlanNode): string {
  if (!node.relation) return ''
  const name = [node.schema, node.relation].filter(Boolean).join('.')
  return node.alias && node.alias !== node.relation ? `${name} ${node.alias}` : name
}

// rows and total_time_ms are per loop; the node produced them loops times.
export function nodeRows(node: PlanNode): number | null {
  if (!node.actual) return null
  return node.actual.rows * node.actual.loops
}

export function nodeTimeMs(node: PlanNode): number | null {
  if (!node.actual || node.actual.total_time_ms == null) return null
  return node.actual.total_time_ms * node.actual.loops
}

export function findingsByPath(findings: PlanFinding[] | undefined): Map<string, PlanFinding[]> {
  const byPath = new Map<string, PlanFinding[]>()
  for (const f of findings ?? []) {
    const key = pathKey(f.path)
    const at = byPath.get(key)
    if (at) at.push(f)
    else byPath.set(key, [f])
  }
  return byPath
}

const SEVERITY_COLOR: Record<string, string> = {
  HIGH: 'error',
  MEDIUM: 'warning',
  LOW: 'info',
}

export function severityColor(severity: string | undefined): string {
  return SEVERITY_COLOR[severity ?? ''] ?? 'grey'
}

const SEVERITY_RANK: Record<string, number> = { HIGH: 0, MEDIUM: 1, LOW: 2 }

export function severityRank(severity: string | undefined): number {
  return SEVERITY_RANK[severity ?? ''] ?? 3
}

function num(v: unknown): number {
  return typeof v === 'number' ? v : 0
}

type TFunc = (key: string, named?: Record<string, unknown>) => string

// Every code gets every parameter — vue-i18n drops the ones its phrasing omits.
// An unknown code falls back to the bare code so the finding still renders.
export function findingText(f: PlanFinding, t: TFunc, te: (key: string) => boolean): string {
  const key = `planFinding.${f.code}`
  if (!te(key)) return f.code

  const p = (f.params ?? {}) as Record<string, unknown>
  return t(key, {
    node: f.node_type,
    relation: f.relation ?? '',
    tableRows: fmtCompact(num(p.table_rows)),
    planRows: fmtCompactFloat(num(p.plan_rows)),
    actualRows: fmtCompactFloat(num(p.actual_rows)),
    scannedRows: fmtCompact(num(p.scanned_rows)),
    ratio: fmtCompactFloat(num(p.ratio)),
    selfCost: fmtCompactFloat(num(p.self_cost)),
    totalCost: fmtCompactFloat(num(p.total_cost)),
    costShare: fmtPct(num(p.cost_share) * 100),
    outerRows: fmtCompactFloat(num(p.outer_rows)),
    innerRows: fmtCompactFloat(num(p.inner_rows)),
    pairs: fmtCompactFloat(num(p.pairs)),
    estimated: fmtBytes(num(p.estimated_kb) * 1024),
    workMem: fmtBytes(num(p.work_mem_kb) * 1024),
    rowsRemoved: fmtCompactFloat(num(p.rows_removed)),
    removedShare: fmtPct(num(p.removed_share) * 100),
    heapFetches: fmtCompactFloat(num(p.heap_fetches)),
    sortMethod: String(p.sort_method ?? ''),
    sortSpace: fmtBytes(num(p.sort_space_kb) * 1024),
    loops: fmtCompactFloat(num(p.loops)),
    expectedLoops: fmtCompactFloat(num(p.expected_loops)),
    lossyBlocks: fmtCompactFloat(num(p.lossy_blocks)),
    exactBlocks: fmtCompactFloat(num(p.exact_blocks)),
    workersPlanned: num(p.workers_planned),
    workersLaunched: num(p.workers_launched),
    timeMs: fmtCompactFloat(num(p.time_ms)),
    totalTimeMs: fmtCompactFloat(num(p.total_time_ms)),
    timeShare: fmtPct(num(p.time_share) * 100),
  })
}

