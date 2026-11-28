import { describe, expect, it } from 'vitest'

import type { ReplicationSlot } from '@/api/models/index'
import { hasFailoverColumns, hasSyncColumn, isKnownSkipReason, showsSyncState } from '../slotSync'
import { recoveryAttention } from '../recovery'

function slot(over: Partial<ReplicationSlot> = {}): ReplicationSlot {
  return { SlotName: 's', SlotType: 'logical', Active: true, ...over }
}

describe('slot sync columns', () => {
  it('hides failover/synced before PG 17', () => {
    expect(hasFailoverColumns([slot(), slot({ Failover: null, Synced: null })])).toBe(false)
    expect(hasFailoverColumns([slot({ Failover: false, Synced: false })])).toBe(true)
  })

  it('shows the sync state only for synced slots on a standby', () => {
    expect(showsSyncState(slot({ Synced: true, OnStandby: true }), true)).toBe(true)
    expect(showsSyncState(slot({ Synced: true, OnStandby: false }), true)).toBe(false)
    expect(showsSyncState(slot({ Synced: false, OnStandby: true }), true)).toBe(false)
    expect(hasSyncColumn([slot({ Synced: false, OnStandby: true })], true)).toBe(false)
  })

  it('hides the sync state before PG 19, where the reason is always null', () => {
    const synced = slot({ Synced: true, OnStandby: true, SlotsyncSkipReason: null })
    expect(showsSyncState(synced, false)).toBe(false)
    expect(hasSyncColumn([synced], false)).toBe(false)
  })

  it('decodes the known reasons and passes others through', () => {
    expect(isKnownSkipReason('wal_not_flushed')).toBe(true)
    expect(isKnownSkipReason('something_new')).toBe(false)
  })
})

describe('recoveryAttention', () => {
  it('highlights a pause and a promotion', () => {
    expect(recoveryAttention({ state: 'ok', pause_state: 'paused' })).toBe(true)
    expect(recoveryAttention({ state: 'ok', pause_state: 'pause requested' })).toBe(true)
    expect(recoveryAttention({ state: 'ok', pause_state: 'not paused', promote_triggered: true })).toBe(true)
    expect(recoveryAttention({ state: 'ok', pause_state: 'not paused', promote_triggered: false })).toBe(false)
    expect(recoveryAttention({ state: 'no_privilege' })).toBe(false)
  })
})
