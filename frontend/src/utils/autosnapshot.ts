export function outcomeI18nKey(outcome: string): string {
  return `autosnapshot.outcome.${outcome.replaceAll(':', '_')}`
}

export function triggerI18nKey(triggerType: string): string {
  return `autosnapshot.trigger.${triggerType}`
}
