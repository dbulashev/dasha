// The red band (< 40) is what the backend's critical floor targets
// (health.criticalScoreCeiling = 30); keep these thresholds in sync with it.
export function scoreColor(score: number): string {
  if (score >= 95) return 'success'
  if (score >= 70) return 'warning'
  if (score >= 40) return 'orange'
  return 'error'
}
