import { describe, expect, it } from 'vitest'

import { fmtCompactFloat, fmtWindow } from '@/utils/format'

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

describe('fmtCompactFloat', () => {
  it('keeps fractional values compact instead of locale-grouping them', () => {
    expect(fmtCompactFloat(2312.393)).toBe('2.3k')
    expect(fmtCompactFloat(19500)).toBe('19.5k')
    expect(fmtCompactFloat(2_400_000)).toBe('2.4M')
  })

  it('drops decimals once the integer part carries the magnitude', () => {
    expect(fmtCompactFloat(12.4)).toBe('12')
    expect(fmtCompactFloat(0.62)).toBe('0.6')
    expect(fmtCompactFloat(0)).toBe('0')
  })

  it('returns the placeholder for a missing value', () => {
    expect(fmtCompactFloat(null)).toBe('—')
  })
})
