import { ref } from 'vue'

import { getIOHistory } from '@/api/gen/default/default'
import type { IOHistory } from '@/api/models/index'
import { useViewError } from '@/composables/useViewError'
import { ApiError, assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

// Range key -> span in seconds.
export const IO_RANGES: Record<string, number> = {
  '1h': 3600,
  '6h': 6 * 3600,
  '24h': 24 * 3600,
  '7d': 7 * 24 * 3600,
}

const POINTS = 200

export interface IoHistoryQuery {
  clusterName: string
  hostName: string
  rangeKey: string
  object: string
  groupBy: 'context' | 'backend_type'
  backendType: string | null
  context: string | null
}

// Historical mode: the chart series and the matrix of one host over a period.
export function useIoHistory() {
  const history = ref<IOHistory | null>(null)
  const matrix = ref<IOHistory | null>(null)
  const loading = ref(false)
  const unavailable = ref(false)

  const { onError } = useViewError()

  let requestId = 0

  function clear() {
    requestId++
    history.value = null
    matrix.value = null
    unavailable.value = false
    loading.value = false
  }

  async function load(query: IoHistoryQuery) {
    const id = ++requestId
    loading.value = true

    const to = new Date()
    const from = new Date(to.getTime() - IO_RANGES[query.rangeKey] * 1000)

    const common = {
      cluster_name: query.clusterName,
      instance: query.hostName,
      from: from.toISOString(),
      to: to.toISOString(),
    }

    try {
      const [series, detail] = await Promise.all([
        getIOHistory({
          ...common,
          group_by: query.groupBy,
          points: POINTS,
          object: query.object,
          backend_type: query.backendType ?? undefined,
          context: query.context ?? undefined,
        }),
        // The matrix is the same endpoint collapsed to one interval; it stays
        // unfiltered so every dimension keeps its full set of values — the
        // filters narrow it on the client.
        getIOHistory({ ...common, group_by: 'full', points: 1 }),
      ])

      if (id !== requestId) return

      history.value = assertOk<IOHistory>(series)
      matrix.value = assertOk<IOHistory>(detail)
    } catch (err) {
      if (id !== requestId) return

      history.value = null
      matrix.value = null

      if (err instanceof ApiError && err.status === 501) {
        unavailable.value = true
        return
      }

      onError(getErrorMessage(err), err)
    } finally {
      if (id === requestId) loading.value = false
    }
  }

  return { history, matrix, loading, unavailable, load, clear }
}
