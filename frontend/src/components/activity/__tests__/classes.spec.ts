import { describe, expect, it } from 'vitest'

import { classColor, eventClass, eventColor, SlotAssigner } from '../classes'

describe('SlotAssigner', () => {
  it('keeps a key on its slot when the ranking changes', () => {
    const a = new SlotAssigner()

    expect(a.assign(['x', 'y', 'z'])).toEqual(new Map([['x', 0], ['y', 1], ['z', 2]]))
    expect(a.assign(['z', 'x', 'y'])).toEqual(new Map([['x', 0], ['y', 1], ['z', 2]]))
  })

  it('reuses the slot of a key that left the chart', () => {
    const a = new SlotAssigner()

    a.assign(['x', 'y'])
    const next = a.assign(['y', 'w'])

    expect(next.get('y')).toBe(1)
    expect(next.get('w')).toBe(0)
    expect(next.has('x')).toBe(false)
  })
})

describe('eventColor', () => {
  it('paints an event in the color of its class', () => {
    expect(eventClass('Lock:transactionid')).toBe('Lock')
    expect(eventClass('CPU:')).toBe('CPU')
    expect(eventColor('CPU:', 0, false)).toBe(classColor('CPU', false))
    expect(eventColor('Lock:transactionid', 0, true)).toBe(classColor('Lock', true))
  })

  it('gives the next event of the same class a different shade', () => {
    const first = eventColor('Lock:transactionid', 0, false)
    const second = eventColor('Lock:tuple', 1, false)

    expect(second).not.toBe(first)
    expect(second).toMatch(/^#[0-9a-f]{6}$/)
  })
})
