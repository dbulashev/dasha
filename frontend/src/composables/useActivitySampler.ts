import { onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'

import { getActivitySample } from '@/api/gen/default/default'
import type { ActivitySample } from '@/api/models/activitySample'
import type { ActivityBuffer } from '@/components/activity/activityBuffer'
import { ApiError, assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

export const ERROR_STREAK_BANNER = 3
export const IDLE_STOP_MS = 10 * 60_000

const INPUT_EVENTS = ['pointermove', 'pointerdown', 'keydown', 'wheel', 'touchstart', 'scroll'] as const

export interface ActivityAccess {
  canReadAllStats: boolean
  queryIdAvailable: boolean
  truncated: boolean
}

// Polls one host: the next request goes out a period after the previous one
// started, and never before it answered.
export function useActivitySampler(opts: {
  cluster: Ref<string | null>
  host: Ref<string | null>
  periodSec: Ref<number>
  buffer: ActivityBuffer
  onChange: () => void
}) {
  const unsupported = ref(false)
  const lastError = ref<string | null>(null)
  const errorStreak = ref(0)
  const access = ref<ActivityAccess | null>(null)
  const idleStopped = ref(false)
  const paused = ref(false)
  let lastInput = Date.now()

  function onInput() {
    lastInput = Date.now()
  }

  let generation = 0
  let timer: ReturnType<typeof setTimeout> | null = null
  let textSince: string | null = null

  function hidden(): boolean {
    return typeof document !== 'undefined' && document.visibilityState === 'hidden'
  }

  function clearTimer() {
    if (timer !== null) clearTimeout(timer)
    timer = null
  }

  function stop() {
    generation++
    clearTimer()
  }

  function start() {
    stop()
    idleStopped.value = false
    lastInput = Date.now()
    if (paused.value || !opts.cluster.value || !opts.host.value || unsupported.value || hidden()) return
    void tick(generation)
  }

  async function tick(gen: number) {
    const cluster = opts.cluster.value
    const host = opts.host.value
    if (gen !== generation || !cluster || !host) return

    const startedAt = performance.now()

    try {
      const res = await getActivitySample({
        cluster_name: cluster,
        instance: host,
        include_background: true,
        text_since: textSince ?? undefined,
      })
      if (gen !== generation) return

      const data = assertOk<ActivitySample>(res)
      opts.buffer.push(data)
      textSince = data.sampled_at
      access.value = {
        canReadAllStats: data.can_read_all_stats,
        queryIdAvailable: data.query_id_available,
        truncated: data.truncated,
      }
      errorStreak.value = 0
      lastError.value = null
    } catch (err) {
      if (gen !== generation) return

      if (err instanceof ApiError && err.status === 501) {
        unsupported.value = true
        stop()
        return
      }

      opts.buffer.pushGap()
      textSince = null
      errorStreak.value++
      lastError.value = getErrorMessage(err)
    }

    opts.onChange()

    if (Date.now() - lastInput >= IDLE_STOP_MS) {
      stop()
      idleStopped.value = true
      textSince = null
      return
    }

    const elapsed = performance.now() - startedAt
    timer = setTimeout(() => void tick(gen), Math.max(0, opts.periodSec.value * 1000 - elapsed))
  }

  function onVisibility() {
    if (hidden()) {
      stop()
      textSince = null
    } else {
      start()
    }
  }

  watch(
    [opts.cluster, opts.host],
    () => {
      opts.buffer.clear()
      textSince = null
      unsupported.value = false
      access.value = null
      errorStreak.value = 0
      lastError.value = null
      paused.value = false
      opts.onChange()
      start()
    },
    { immediate: true },
  )

  function pause() {
    stop()
    paused.value = true
    textSince = null
  }

  function resume() {
    paused.value = false
    textSince = null
    start()
  }

  onMounted(() => {
    document.addEventListener('visibilitychange', onVisibility)
    for (const e of INPUT_EVENTS) window.addEventListener(e, onInput, { passive: true, capture: true })
  })
  onBeforeUnmount(() => {
    document.removeEventListener('visibilitychange', onVisibility)
    for (const e of INPUT_EVENTS) window.removeEventListener(e, onInput, { capture: true })
    stop()
  })

  return { unsupported, lastError, errorStreak, access, idleStopped, paused, pause, resume }
}
