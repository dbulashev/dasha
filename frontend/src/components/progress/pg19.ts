// PG 19 progress columns: who started a VACUUM/ANALYZE and the VACUUM mode.

export type VacuumWarning = 'failsafe' | 'wraparound'

// failsafe wins: it is the stronger signal and the one that can appear mid-run.
export function vacuumWarning(
  startedBy: string | null | undefined,
  mode: string | null | undefined,
): VacuumWarning | null {
  if (mode === 'failsafe') return 'failsafe'
  if (startedBy === 'autovacuum_wraparound') return 'wraparound'
  return null
}

export function startedByColor(startedBy: string): string {
  if (startedBy === 'autovacuum_wraparound') return 'warning'
  if (startedBy === 'autovacuum') return 'info'
  return 'default'
}

export function modeColor(mode: string): string {
  if (mode === 'failsafe') return 'warning'
  if (mode === 'aggressive') return 'info'
  return 'default'
}
