import { describe, expect, it } from 'vitest'

import { vacuumWarning } from '../pg19'

describe('vacuumWarning', () => {
  it('warns on failsafe and on wraparound, failsafe first', () => {
    expect(vacuumWarning('autovacuum_wraparound', 'failsafe')).toBe('failsafe')
    expect(vacuumWarning('autovacuum', 'failsafe')).toBe('failsafe')
    expect(vacuumWarning('autovacuum_wraparound', 'aggressive')).toBe('wraparound')
    expect(vacuumWarning('manual', 'normal')).toBeNull()
    expect(vacuumWarning(null, null)).toBeNull()
  })
})
