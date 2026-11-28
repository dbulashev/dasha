import { ref } from 'vue'

import { getLockStatsHistory } from '@/api/gen/default/default'
import type { FeatureUnavailableReason, LockStatsHistory } from '@/api/models/index'
import { assertOk, featureUnavailableReason } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

// Range key -> span in seconds.
export const LOCK_STATS_RANGES: Record<string, number> = {
  '1h': 3600,
  '6h': 6 * 3600,
  '24h': 24 * 3600,
  '7d': 7 * 24 * 3600,
}

const POINTS = 200

export interface LockStatsQuery {
  clusterName: string
  hostName: string
  rangeKey: string
}

// The chart series and the totals of the window (the same endpoint with points=1).
export function useLockStatsHistory() {
  const history = ref<LockStatsHistory | null>(null)
  const totals = ref<LockStatsHistory | null>(null)
  const loading = ref(false)
  const unavailable = ref<FeatureUnavailableReason | null>(null)
  // Kept in the section: the global error would replace the whole page.
  const error = ref<string | null>(null)

  let requestId = 0

  function clear() {
    requestId++
    history.value = null
    totals.value = null
    unavailable.value = null
    error.value = null
    loading.value = false
  }

  async function load(query: LockStatsQuery) {
    const id = ++requestId
    loading.value = true
    error.value = null

    const to = new Date()
    const from = new Date(to.getTime() - LOCK_STATS_RANGES[query.rangeKey] * 1000)

    const common = {
      cluster_name: query.clusterName,
      instance: query.hostName,
      from: from.toISOString(),
      to: to.toISOString(),
    }

    try {
      const [series, window] = await Promise.all([
        getLockStatsHistory({ ...common, points: POINTS }),
        getLockStatsHistory({ ...common, points: 1 }),
      ])

      if (id !== requestId) return

      history.value = assertOk<LockStatsHistory>(series)
      totals.value = assertOk<LockStatsHistory>(window)
      unavailable.value = null
    } catch (err) {
      if (id !== requestId) return

      history.value = null
      totals.value = null

      const reason = featureUnavailableReason(err, 'no_storage')
      if (reason) {
        unavailable.value = reason
        return
      }

      error.value = getErrorMessage(err)
    } finally {
      if (id === requestId) loading.value = false
    }
  }

  return { history, totals, loading, unavailable, error, load, clear }
}
