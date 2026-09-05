import { ref } from 'vue'
import { describe, expect, it } from 'vitest'

import type { IOHistory } from '@/api/models/index'
import { useIoRows } from '../useIoRows'

function rows(history: IOHistory | null, live = false) {
  return useIoRows({
    live: ref(live),
    liveRows: ref([]),
    liveWindowSeconds: ref(0),
    liveTrackIoTiming: ref(false),
    liveTrackWalIoTiming: ref(true),
    history: ref(history),
    matrix: ref(history),
    backendType: ref(null),
    context: ref(null),
  })
}

function history(trackIoTiming: boolean, trackWalIoTiming: boolean): IOHistory {
  return {
    meta: {
      instance: 'h1',
      earliest_at: null,
      latest_at: null,
      track_io_timing: trackIoTiming,
      track_io_timing_changed: false,
      track_wal_io_timing: trackWalIoTiming,
      track_wal_io_timing_changed: false,
      version_changed: false,
    },
    series: [
      {
        key: { backend_type: 'client backend', object: 'wal', context: 'normal' },
        points: [
          {
            from: '2026-08-29T09:00:00Z',
            to: '2026-08-29T10:00:00Z',
            duration_seconds: 3600,
            complete: true,
            values: { writes: 40, write_time: 120 },
          },
        ],
      },
    ],
  } as IOHistory
}

describe('useIoRows timing flags', () => {
  it('reads the two settings apart', () => {
    const r = rows(history(false, true))

    expect(r.trackIoTiming.value).toBe(false)
    expect(r.trackWalIoTiming.value).toBe(true)
  })

  it('takes both from the live poller in live mode', () => {
    const r = rows(history(true, false), true)

    expect(r.trackIoTiming.value).toBe(false)
    expect(r.trackWalIoTiming.value).toBe(true)
  })

  // No capture cannot read a setting: relation timing stays optimistic so the
  // Time mode is not greyed out on an empty period.
  it('claims nothing from an empty period', () => {
    const r = rows(null)

    expect(r.trackIoTiming.value).toBe(true)
    expect(r.trackWalIoTiming.value).toBe(false)
  })
})
