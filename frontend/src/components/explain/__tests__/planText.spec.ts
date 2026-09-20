import { describe, expect, it } from 'vitest'

import type { PlanFinding, PlanNode } from '@/api/models'
import {
  findingText,
  findingsByPath,
  flattenPlan,
  nodeLabel,
  nodeRelation,
  nodeRows,
  nodeTimeMs,
} from '../planText'

interface RawFinding {
  code: string
  severity: string
  path: number[]
  node_type: string
  relation?: string
  params?: Record<string, unknown>
}

// code stays a plain string here: a rule the backend adds before its phrasing
// lands is not part of the generated union, and the fallback is what this
// suite checks.
function finding(raw: RawFinding): PlanFinding {
  return raw as PlanFinding
}

function node(partial: Partial<PlanNode>): PlanNode {
  return {
    type: 'Seq Scan',
    parallel: false,
    startup_cost: 0,
    total_cost: 1,
    plan_rows: 1,
    plan_width: 8,
    children: [],
    ...partial,
  }
}

describe('flattenPlan', () => {
  it('numbers every node by the child path findings carry', () => {
    const root = node({
      type: 'Hash Join',
      children: [node({ type: 'Seq Scan' }), node({ type: 'Hash', children: [node({})] })],
    })

    expect(flattenPlan(root).map(n => [n.key, n.node.type, n.depth])).toEqual([
      ['', 'Hash Join', 0],
      ['0', 'Seq Scan', 1],
      ['1', 'Hash', 1],
      ['1.0', 'Seq Scan', 2],
    ])
  })
})

describe('nodeLabel', () => {
  it('puts back the modifiers the parser split off', () => {
    expect(nodeLabel(node({ type: 'Seq Scan', parallel: true, partial_mode: 'Partial' })))
      .toBe('Partial Parallel Seq Scan')
    expect(nodeLabel(node({ type: 'Aggregate', strategy: 'Hashed' }))).toBe('Aggregate (Hashed)')
    expect(nodeLabel(node({ type: 'Hash Join', join_type: 'Left' }))).toBe('Hash Join (Left)')
  })

  it('leaves out what PostgreSQL does not print', () => {
    expect(nodeLabel(node({ type: 'Index Scan', join_type: 'Inner', scan_direction: 'Forward' })))
      .toBe('Index Scan')
  })
})

describe('nodeRelation', () => {
  it('keeps the alias only when it differs from the relation', () => {
    expect(nodeRelation(node({ schema: 'public', relation: 'orders', alias: 'o' })))
      .toBe('public.orders o')
    expect(nodeRelation(node({ schema: 'public', relation: 'orders', alias: 'orders' })))
      .toBe('public.orders')
  })
})

describe('per-loop measurements', () => {
  it('multiplies rows and time by the loops of the node', () => {
    const n = node({ actual: { rows: 10, loops: 5, total_time_ms: 2 } })
    expect(nodeRows(n)).toBe(50)
    expect(nodeTimeMs(n)).toBe(10)
  })

  it('reports nothing where the plan carries no measurements', () => {
    expect(nodeRows(node({}))).toBeNull()
    expect(nodeTimeMs(node({ actual: { rows: 1, loops: 1 } }))).toBeNull()
  })
})

describe('findingsByPath', () => {
  it('groups several findings of one node under its path', () => {
    const findings = [
      finding({ code: 'seq_scan_large', severity: 'MEDIUM', path: [0, 1], node_type: 'Seq Scan' }),
      finding({ code: 'row_misestimate', severity: 'HIGH', path: [0, 1], node_type: 'Seq Scan' }),
      finding({ code: 'cost_hotspot', severity: 'LOW', path: [], node_type: 'Hash Join' }),
    ]

    const byPath = findingsByPath(findings)
    expect(byPath.get('0.1')?.map(f => f.code)).toEqual(['seq_scan_large', 'row_misestimate'])
    expect(byPath.get('')?.map(f => f.code)).toEqual(['cost_hotspot'])
  })
})

describe('findingText', () => {
  const t = (key: string, named?: Record<string, unknown>) =>
    `${key}|${JSON.stringify(named?.ratio)}|${JSON.stringify(named?.actualRows)}`

  it('renders the code through i18n with its parameters', () => {
    const f = finding({
      code: 'row_misestimate',
      severity: 'HIGH',
      path: [],
      node_type: 'Seq Scan',
      params: { ratio: 120, actual_rows: 12000 },
    })

    expect(findingText(f, t, () => true)).toBe('planFinding.row_misestimate|"120"|"12k"')
  })

  it('falls back to the bare code when the phrasing is missing', () => {
    const f = finding({ code: 'brand_new_rule', severity: 'LOW', path: [], node_type: 'Sort' })
    expect(findingText(f, t, () => false)).toBe('brand_new_rule')
  })
})
