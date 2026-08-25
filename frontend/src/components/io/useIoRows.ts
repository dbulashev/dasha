import { computed, type Ref } from 'vue'

import type { IOHistory } from '@/api/models/index'
import { BUFFER_OBJECTS, VISIBLE_OBJECTS, type MatrixRow } from './types'

// The matrix and everything read off it, from whichever mode is on screen.
export function useIoRows(source: {
  live: Ref<boolean>
  liveRows: Ref<MatrixRow[]>
  liveWindowSeconds: Ref<number>
  liveTrackIoTiming: Ref<boolean>
  history: Ref<IOHistory | null>
  matrix: Ref<IOHistory | null>
  backendType: Ref<string | null>
  context: Ref<string | null>
}) {
  const historyRows = computed<MatrixRow[]>(() =>
    (source.matrix.value?.series ?? []).map((s) => ({
      backend_type: s.key.backend_type ?? '',
      object: s.key.object ?? '',
      context: s.key.context ?? '',
      values: { ...(s.points[0]?.values ?? {}) },
    })),
  )

  const rawRows = computed<MatrixRow[]>(() =>
    source.live.value ? source.liveRows.value : historyRows.value,
  )

  // v1 covers buffered relation I/O; PG 18's WAL rows wait for a card of their own.
  const rows = computed(() =>
    rawRows.value.filter((r) => {
      if (!VISIBLE_OBJECTS.includes(r.object)) return false
      if (source.context.value && r.context !== source.context.value) return false
      if (source.backendType.value && r.backend_type !== source.backendType.value) return false
      return true
    }),
  )

  const bufferRows = computed(() => rows.value.filter((r) => BUFFER_OBJECTS.includes(r.object)))

  // WAL rows exist from PostgreSQL 18 on; an older host must not be offered the tab.
  const availableObjects = computed(() => {
    const seen = new Set(rawRows.value.map((r) => r.object))
    return VISIBLE_OBJECTS.filter((o) => o === 'relation' || seen.has(o))
  })

  const backendTypes = computed(() => {
    const seen = new Set(
      rawRows.value.filter((r) => VISIBLE_OBJECTS.includes(r.object)).map((r) => r.backend_type),
    )
    return [...seen].sort()
  })

  const windowSeconds = computed(() =>
    source.live.value
      ? source.liveWindowSeconds.value
      : (source.matrix.value?.series?.[0]?.points?.[0]?.duration_seconds ?? 0),
  )

  // An empty period reports track_io_timing as false simply because there is no
  // capture to read it from; that must not look like the setting being off.
  const trackIoTiming = computed(() => {
    if (source.live.value) return source.liveTrackIoTiming.value
    if (!source.history.value?.series?.length) return true
    return source.history.value.meta.track_io_timing
  })

  const partial = computed(
    () =>
      !source.live.value &&
      (source.history.value?.series ?? []).some((s) => s.points.some((p) => !p.complete)),
  )

  const noData = computed(() => rows.value.length === 0)

  return {
    rows,
    bufferRows,
    availableObjects,
    backendTypes,
    windowSeconds,
    trackIoTiming,
    partial,
    noData,
  }
}
