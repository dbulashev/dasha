import { describe, expect, it } from 'vitest'
import type { PlanLifecycleAnalysis, PlanLifecycleIndexChain, PlanLifecyclePath } from '@/api/models'
import { fmtCost, fmtRatio, indexVerdict, pathName, termText } from '../planlife'

const t = (key: string, params?: Record<string, unknown>) => (params ? `${key} ${JSON.stringify(params)}` : key)
const aliases = new Map([
  [1, 'c'],
  [2, 'o'],
])

function path(p: Partial<PlanLifecyclePath>): PlanLifecyclePath {
  return { path_id: 1, pathtype: 'SeqScan', rel: '{1}', fate: 'survived', events: [], parents: [], ...p } as PlanLifecyclePath
}

describe('fmtCost', () => {
  it('keeps two decimals below a thousand', () => {
    expect(fmtCost(0.71)).toBe('0.71')
    expect(fmtCost(178.78)).toBe('178.78')
  })

  it('compacts from a thousand', () => {
    expect(fmtCost(31761.4)).toBe('31.8k')
    expect(fmtCost(144470180)).toBe('144.5M')
  })
})

describe('fmtRatio', () => {
  it('one decimal below ten, compact above', () => {
    expect(fmtRatio(2.715)).toBe('×2.7')
    expect(fmtRatio(2362)).toBe('×2.4k')
  })
})

describe('pathName', () => {
  it('names a scan by its alias and index', () => {
    const p = path({ pathtype: 'IndexOnlyScan', index_name: 'pp_customers_pkey' })
    expect(pathName(p, aliases, t)).toBe('Index Only Scan c (pp_customers_pkey)')
  })

  it('adds the join relation only when asked', () => {
    const p = path({ pathtype: 'NestLoop', rel: '{1,2}' })
    expect(pathName(p, aliases, t)).toBe('Nested Loop')
    expect(pathName(p, aliases, t, true)).toBe('Nested Loop c ⋈ o')
  })
})

describe('termText', () => {
  it('falls back to the raw value without a translation', () => {
    const te = (k: string) => k === 'planLifecycle.keptReason.order_'
    expect(termText('keptReason', 'order?', t, te)).toBe('planLifecycle.keptReason.order_')
    expect(termText('skipReason', 'brand_new', t, te)).toBe('brand_new')
  })
})

describe('indexVerdict', () => {
  const a = {
    final: { path_id: 24, status: 'exact', candidates: [24], tree: [24] },
    paths: [
      path({ path_id: 24, pathtype: 'HashJoin', rel: '{1,2}', fate: 'chosen', cost_total: 31761.4 }),
      path({ path_id: 11, pathtype: 'NestLoop', rel: '{1,2}', fate: 'displaced', cost_total: 86234.27 }),
      path({ path_id: 8, pathtype: 'IndexScan', rel: '{2}', cost_total: 16.21 }),
    ],
  } as unknown as PlanLifecycleAnalysis
  const ix = { index: 'pp_orders_customer_idx', outcome: 'lost', hypothetical: false, leaves: [], truncated: false }

  it('compares the best path with the chosen one at the same level', () => {
    const v = indexVerdict({ ...ix, best_path_id: 11, best_at_final_level: true } as PlanLifecycleIndexChain, a, aliases, t)
    expect(v).toContain('summary.lostAtTop')
    expect(v).toContain('"ratio":"×2.7"')
    expect(v).toContain('Nested Loop c ⋈ o #11')
  })

  it('names the level the paths stopped at', () => {
    const v = indexVerdict({ ...ix, best_path_id: 8, best_at_final_level: false } as PlanLifecycleIndexChain, a, aliases, t)
    expect(v).toContain('summary.stoppedAt')
    expect(v).toContain('"rel":"o"')
  })
})
