import { computed, ref, type Ref } from 'vue'

import { getIOCurrent } from '@/api/gen/default/default'
import type { IOSnapshot } from '@/api/models/index'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { useViewError } from '@/composables/useViewError'
import { ApiError, assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import type { MatrixRow } from './types'

const MAX_DURATION_MS = 5 * 60 * 1000

function rowKey(backendType: string, object: string, context: string): string {
  return `${backendType}|${object}|${context}`
}

// A reset or a major upgrade makes the two slices incomparable.
function sameEpoch(a: IOSnapshot, b: IOSnapshot): boolean {
  if (Math.trunc(a.version_num / 10000) !== Math.trunc(b.version_num / 10000)) return false
  return (a.stats_reset ?? null) === (b.stats_reset ?? null)
}

// Live mode: polls one host and reports the difference between the last two
// slices, the way top and pgcenter do.
export function useIoLive(source: {
  clusterName: Ref<string | null>
  hostName: Ref<string | null>
  intervalSec: Ref<number>
}) {
  const previous = ref<IOSnapshot | null>(null)
  const current = ref<IOSnapshot | null>(null)
  const loading = ref(false)
  const unsupported = ref(false)

  const { onError } = useViewError()

  // Bumped on every buffer drop and every poll: neither a poll of the previous
  // host nor an earlier overlapping poll may land in the new baseline — the epoch
  // check compares versions and stats_reset, which two different servers can share.
  let pollId = 0

  const { active, remaining, start, stop, toggle } = useAutoRefresh({
    pollInterval: () => source.intervalSec.value * 1000,
    maxDuration: MAX_DURATION_MS,
    onTick: () => load(),
  })

  function push(snapshot: IOSnapshot) {
    previous.value = current.value && sameEpoch(current.value, snapshot) ? current.value : null
    current.value = snapshot
  }

  function reset() {
    pollId++
    previous.value = null
    current.value = null
    unsupported.value = false
    loading.value = false
  }

  async function load() {
    const cluster = source.clusterName.value
    const host = source.hostName.value
    if (!cluster || !host) return

    const id = ++pollId
    loading.value = true

    try {
      const res = await getIOCurrent({ cluster_name: cluster, instance: host })
      if (id !== pollId) return

      push(assertOk<IOSnapshot>(res))
      unsupported.value = false
    } catch (err) {
      if (id !== pollId) return

      if (err instanceof ApiError && err.status === 501) {
        // A host without pg_stat_io answers nothing; polling repeats the 501.
        unsupported.value = true
        stop()
        return
      }

      onError(getErrorMessage(err), err)
    } finally {
      if (id === pollId) loading.value = false
    }
  }

  const windowSeconds = computed(() => {
    if (!previous.value || !current.value) return 0
    const ms = Date.parse(current.value.captured_at) - Date.parse(previous.value.captured_at)
    return ms > 0 ? ms / 1000 : 0
  })

  const rows = computed<MatrixRow[]>(() => {
    const prev = previous.value
    const cur = current.value
    if (!prev || !cur) return []

    const base = new Map<string, Record<string, number>>()
    for (const r of prev.rows) base.set(rowKey(r.backend_type, r.object, r.context), r.values)

    const out: MatrixRow[] = []

    for (const r of cur.rows) {
      const before = base.get(rowKey(r.backend_type, r.object, r.context))
      if (!before) continue

      const values: Record<string, number> = {}
      for (const [name, value] of Object.entries(r.values)) {
        const previousValue = before[name]
        if (previousValue === undefined) continue
        // A counter that fell means a reset the stats_reset check missed.
        if (value < previousValue) return []
        values[name] = value - previousValue
      }

      out.push({
        backend_type: r.backend_type,
        object: r.object,
        context: r.context,
        values,
      })
    }

    return out
  })

  return {
    rows,
    windowSeconds,
    loading,
    unsupported,
    active,
    remaining,
    load,
    reset,
    start,
    stop,
    toggle,
    // True right after opening or a baseline reset: one slice is not a rate yet.
    waiting: computed(() => current.value !== null && previous.value === null),
    lastAt: computed(() => current.value?.captured_at ?? null),
    trackIoTiming: computed(() => current.value?.track_io_timing ?? true),
  }
}
