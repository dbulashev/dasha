import { describe, expect, it } from 'vitest'

import { fmtIoTime, fmtMetricRate } from '../types'

describe('fmtIoTime', () => {
  it('picks the unit from the magnitude', () => {
    expect(fmtIoTime(16_700_000)).toBe('4.6 h')
    expect(fmtIoTime(90_000)).toBe('1.5 min')
    expect(fmtIoTime(12_000)).toBe('12 s')
    expect(fmtIoTime(350)).toBe('350 ms')
    expect(fmtIoTime(0.5)).toBe('500 µs')
  })
})

describe('fmtMetricRate', () => {
  it('renders counts per second in compact form', () => {
    expect(fmtMetricRate(2_312_393, 1000, 'count')).toBe('2.3k/s')
  })

  // Summed across backends, I/O time per second can exceed one second.
  it('scales the time rate to its own unit', () => {
    expect(fmtMetricRate(16_700_000, 3600, 'time')).toBe('4.6 s/s')
  })

  it('returns the placeholder for an empty window', () => {
    expect(fmtMetricRate(100, 0, 'count')).toBe('—')
  })
})
