import { computed, ref } from 'vue'

import type { IOSnapshot } from '@/api/models/index'
import type { MatrixRow } from './types'

function rowKey(backendType: string, object: string, context: string): string {
  return `${backendType}|${object}|${context}`
}

// The counters describe one continuous run of one server: a reset or a major
// upgrade makes the two slices incomparable, so the baseline is dropped rather
// than subtracted into nonsense.
function sameEpoch(a: IOSnapshot, b: IOSnapshot): boolean {
  if (Math.trunc(a.version_num / 10000) !== Math.trunc(b.version_num / 10000)) return false
  return (a.stats_reset ?? null) === (b.stats_reset ?? null)
}

/**
 * Live mode: keeps the previous raw slice in memory and reports the difference
 * between the last two ticks, the way top and pgcenter do.
 */
export function useIoLive() {
  const previous = ref<IOSnapshot | null>(null)
  const current = ref<IOSnapshot | null>(null)

  function push(snapshot: IOSnapshot) {
    previous.value = current.value && sameEpoch(current.value, snapshot) ? current.value : null
    current.value = snapshot
  }

  function reset() {
    previous.value = null
    current.value = null
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
    push,
    reset,
    rows,
    windowSeconds,
    // True right after opening or a baseline reset: one slice is not a rate yet.
    waiting: computed(() => current.value !== null && previous.value === null),
    lastAt: computed(() => current.value?.captured_at ?? null),
    trackIoTiming: computed(() => current.value?.track_io_timing ?? true),
  }
}
