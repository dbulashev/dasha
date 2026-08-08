import { describe, expect, it } from 'vitest'

import { fmtWindow } from '@/utils/format'

// Stand-in for vue-i18n's t: returns the last segment so assertions stay readable.
const t = (key: string) => key.split('.').pop() as string

describe('fmtWindow', () => {
  const from = '2026-08-01T00:00:00Z'

  it('renders the largest units first and drops empty ones', () => {
    expect(fmtWindow(from, '2026-08-03T03:10:00Z', t)).toBe('2 d 3 h 10 min')
    expect(fmtWindow(from, '2026-08-03T00:00:00Z', t)).toBe('2 d')
    expect(fmtWindow(from, '2026-08-01T00:10:05Z', t)).toBe('10 min 5 sec')
  })

  it('renders a zero-length window as seconds rather than an empty string', () => {
    expect(fmtWindow(from, from, t)).toBe('0 sec')
  })

  it('returns the unknown label when either end is missing', () => {
    expect(fmtWindow(null, from, t)).toBe('?')
    expect(fmtWindow(from, undefined, t)).toBe('?')
    expect(fmtWindow(from, null, t, 'окно неизвестно')).toBe('окно неизвестно')
  })

  it('returns the unknown label for an unparsable timestamp', () => {
    expect(fmtWindow('not a date', from, t)).toBe('?')
    expect(fmtWindow(from, 'not a date', t)).toBe('?')
  })

  // Arguments are (from, to): a reversed call must not silently render a span.
  it('marks a negative span instead of formatting it', () => {
    expect(fmtWindow('2026-08-03T00:00:00Z', from, t)).toBe('—')
  })
})
