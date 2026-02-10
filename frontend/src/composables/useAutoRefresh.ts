import { ref, onBeforeUnmount } from 'vue'

interface UseAutoRefreshOptions {
  pollInterval?: number
  maxDuration?: number
  onTick: () => void
}

export function useAutoRefresh(options: UseAutoRefreshOptions) {
  const pollInterval = options.pollInterval ?? 5000
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
    }, pollInterval)

    countdownTimer = setInterval(() => {
      remaining.value = Math.max(0, maxDuration - (Date.now() - startTime))
      if (remaining.value <= 0) stop()
    }, 1000)
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

  return { active, remaining, start, stop, toggle, formatRemaining }
}
