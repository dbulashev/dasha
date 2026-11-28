import type { RecoveryStatus } from '@/api/models/index'

export function pauseHighlighted(pauseState: string | null | undefined): boolean {
  return pauseState === 'paused' || pauseState === 'pause requested'
}

export function recoveryAttention(r: RecoveryStatus | null): boolean {
  return !!r && r.state === 'ok' && (pauseHighlighted(r.pause_state) || r.promote_triggered === true)
}
