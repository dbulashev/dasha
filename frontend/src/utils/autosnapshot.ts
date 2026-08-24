export function outcomeI18nKey(outcome: string): string {
  return `autosnapshot.outcome.${outcome.replaceAll(':', '_')}`
}

export function triggerI18nKey(triggerType: string): string {
  return `autosnapshot.trigger.${triggerType}`
}

// Maps a snapshot reason ("manual" or "auto:<trigger_type>") to a trigger label key.
export function snapshotReasonI18nKey(reason: string | null | undefined): string {
  if (!reason || reason === 'manual') return 'autosnapshot.trigger.manual'
  const trig = reason.startsWith('auto:') ? reason.slice(5) : reason
  return `autosnapshot.trigger.${trig}`
}

const COVERAGE_NAMES_SHOWN = 4

export function snapshotCoverage(databases: string[] | null | undefined): string {
  if (!databases || databases.length === 0) return ''
  if (databases.length <= COVERAGE_NAMES_SHOWN) return databases.join(', ')
  return `${databases.slice(0, COVERAGE_NAMES_SHOWN).join(', ')} +${databases.length - COVERAGE_NAMES_SHOWN}`
}

// An empty list means a pre-attribution snapshot, read instance-wide — nothing
// is missing from it.
export function snapshotCovers(databases: string[] | null | undefined, database: string | null | undefined): boolean {
  if (!database || !databases || databases.length === 0) return true
  return databases.includes(database)
}

// Names the extension a snapshot was read through, only when it is not the
// current one. An unknown current source names nothing: it is not evidence of
// a mismatch.
export function foreignStatsSource(
  snapshotSource: string | null | undefined,
  current: string | null | undefined,
): string {
  if (!current || !snapshotSource || snapshotSource === current) return ''

  return snapshotSource
}
