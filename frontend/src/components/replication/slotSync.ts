import type { ReplicationSlot } from '@/api/models/index'

export const SKIP_REASONS = [
  'wal_or_rows_removed',
  'wal_not_flushed',
  'no_consistent_snapshot',
  'slot_invalidated',
] as const

// The failover/synced columns exist from PG 17; before that every value is null.
export function hasFailoverColumns(slots: ReplicationSlot[]): boolean {
  return slots.some((s) => s.Failover != null || s.Synced != null)
}

// The skip reason only means something for a synced slot on a standby, and
// before PG 19 the column is always null, so a null there is no "in sync".
export function showsSyncState(slot: ReplicationSlot, hasSkipReason: boolean): boolean {
  return hasSkipReason && slot.OnStandby === true && slot.Synced === true
}

export function hasSyncColumn(slots: ReplicationSlot[], hasSkipReason: boolean): boolean {
  return slots.some((s) => showsSyncState(s, hasSkipReason))
}

export function isKnownSkipReason(reason: string): boolean {
  return (SKIP_REASONS as readonly string[]).includes(reason)
}
