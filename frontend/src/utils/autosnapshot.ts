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
