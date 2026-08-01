// Shared severity styling. Deliberately the same scale as the HIGH/MEDIUM/LOW
// of health-score recommendations — a second colour system would make two
// unrelated vocabularies out of one idea.
export const LEVEL_COLOR: Record<string, string> = {
  error: 'error',
  warning: 'warning',
  notice: 'info',
}

export const LEVEL_ICON: Record<string, string> = {
  error: 'mdi-alert-circle-outline',
  warning: 'mdi-alert-outline',
  notice: 'mdi-information-outline',
}
