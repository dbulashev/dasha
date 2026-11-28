import type { MaintenanceAutovacuumQueueRow } from '@/api/models/index'

export type ScoreComponent = 'xid' | 'mxid' | 'vacuum' | 'vacuum_insert' | 'analyze'

// Same order as the backend tie-break: wraparound reasons first.
type ScoreField = 'xid_score' | 'mxid_score' | 'vacuum_score' | 'vacuum_insert_score' | 'analyze_score'

export const SCORE_COMPONENTS: readonly { key: ScoreComponent; field: ScoreField }[] = [
  { key: 'xid', field: 'xid_score' },
  { key: 'mxid', field: 'mxid_score' },
  { key: 'vacuum', field: 'vacuum_score' },
  { key: 'vacuum_insert', field: 'vacuum_insert_score' },
  { key: 'analyze', field: 'analyze_score' },
]

export function isDominant(row: MaintenanceAutovacuumQueueRow, key: ScoreComponent): boolean {
  return row.dominant === key
}

export function dominantColor(key: ScoreComponent): string {
  return key === 'xid' || key === 'mxid' ? 'error' : key === 'analyze' ? 'info' : 'warning'
}

// The bar is relative to the page's top score, never shorter than the 1.0 mark.
export function scoreBar(score: number, top: number): number {
  const scale = Math.max(1, top)
  return Math.min(100, Math.max(0, (score / scale) * 100))
}

// Running processes split by started_by (PG 19+); null before 19.
export interface StartedByChip {
  key: 'manual' | 'autovacuum' | 'wraparound'
  value: number
  color: string
}

export function startedByChips(
  manual: number | null | undefined,
  autovacuum: number | null | undefined,
  wraparound?: number | null,
): StartedByChip[] | null {
  if (manual == null && autovacuum == null && wraparound == null) return null
  const chips: StartedByChip[] = [
    { key: 'manual', value: manual ?? 0, color: 'default' },
    { key: 'autovacuum', value: autovacuum ?? 0, color: 'info' },
  ]
  if (wraparound !== undefined) {
    chips.push({ key: 'wraparound', value: wraparound ?? 0, color: (wraparound ?? 0) > 0 ? 'error' : 'default' })
  }
  return chips.filter((c) => c.value > 0)
}
