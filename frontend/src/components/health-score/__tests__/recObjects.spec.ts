import { describe, expect, it } from 'vitest'

import { recObjectName, recObjects } from '../recObjects'

describe('recObjects', () => {
  it('returns null without an objects array', () => {
    expect(recObjects(null)).toBeNull()
    expect(recObjects({})).toBeNull()
    expect(recObjects({ objects: 'x' })).toBeNull()
    expect(recObjects({ objects: [] })).toBeNull()
  })

  it('drops malformed entries', () => {
    const got = recObjects({
      objects: [{ schema: 'public', table: 'a' }, { schema: 'public' }, null, { schema: 1, table: 'b' }],
    })
    expect(got?.objects).toEqual([{ schema: 'public', table: 'a' }])
  })

  it('keeps a positive more and zeroes the rest', () => {
    const objects = [{ schema: 'public', table: 'a' }]
    expect(recObjects({ objects, more: 3 })?.more).toBe(3)
    expect(recObjects({ objects, more: -1 })?.more).toBe(0)
    expect(recObjects({ objects, more: '3' })?.more).toBe(0)
    expect(recObjects({ objects })?.more).toBe(0)
  })
})

describe('recObjectName', () => {
  it('prefixes the database when present', () => {
    expect(recObjectName({ database: 'app', schema: 'public', table: 'orders' })).toBe('app.public.orders')
    expect(recObjectName({ schema: 'public', table: 'orders' })).toBe('public.orders')
  })
})
