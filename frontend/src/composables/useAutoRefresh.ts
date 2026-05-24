import { ref, onBeforeUnmount } from 'vue'

interface UseAutoRefreshOptions {
  // Static interval in ms, or a getter for reactive intervals (e.g. () => intervalSec.value * 1000).
  pollInterval?: number | (() => number)
  maxDuration?: number
  onTick: () => void
}

export function useAutoRefresh(options: UseAutoRefreshOptions) {
  const pollIntervalGetter = (): number => {
    const v = options.pollInterval
    if (typeof v === 'function') return v()
    return v ?? 5000
  }
  const maxDuration = options.maxDuration ?? 5 * 60 * 1000

  const active = ref(false)
  const remaining = ref(0)

  let pollTimer: ReturnType<typeof setInterval> | null = null
  let countdownTimer: ReturnType<typeof setInterval> | null = null
  let startTime = 0

  function start() {
    stop()
    active.value = true
    startTime = Date.now()
    remaining.value = maxDuration

    pollTimer = setInterval(() => {
      if (Date.now() - startTime >= maxDuration) {
        stop()
        return
      }
      options.onTick()
    }, pollIntervalGetter())

    countdownTimer = setInterval(() => {
      remaining.value = Math.max(0, maxDuration - (Date.now() - startTime))
      if (remaining.value <= 0) stop()
    }, 1000)
  }

  // Restart the poll timer with the current interval (callable from a watcher when interval changes).
  function restart() {
    if (active.value) start()
  }

  function stop() {
    active.value = false
    remaining.value = 0
    if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
    if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
  }

  function toggle() {
    if (active.value) { stop() } else { start() }
  }

  function formatRemaining(ms: number): string {
    const totalSec = Math.ceil(ms / 1000)
    const m = Math.floor(totalSec / 60)
    const s = totalSec % 60
    return `${m}:${s.toString().padStart(2, '0')}`
  }

  onBeforeUnmount(stop)

  return { active, remaining, start, stop, restart, toggle, formatRemaining }
}
