import { describe, expect, it } from 'vitest'

import type { Cluster } from '@/api/models'
import { hostItems } from '@/utils/clusters'
import { assignDigits, hostForDigit, type HostRole, type HostRoleState } from '@/utils/palette/digits'

function roles(...states: [string, HostRoleState][]): HostRole[] {
  return states.map(([host, state]) => ({ host, state }))
}

function digitsOf(hosts: HostRole[]): Record<number, string> {
  const a = assignDigits(hosts)
  if (a.status !== 'ready') throw new Error('pending')
  return Object.fromEntries(a.digits)
}

describe('assignDigits', () => {
  it('gives 1 to a lone host whatever its role, even while loading', () => {
    const states = ['loading', 'failed', 'replica', 'primary'] as const
    expect(states.map(state => digitsOf(roles(['pg-01', state])))).toEqual(states.map(() => ({ 1: 'pg-01' })))
  })

  it('gives 1 to the primary and 2… to replicas in order', () => {
    const hosts = roles(['pg-01', 'replica'], ['pg-02', 'primary'], ['pg-03', 'replica'])
    expect(digitsOf(hosts)).toEqual({ 1: 'pg-02', 2: 'pg-01', 3: 'pg-03' })
    expect((assignDigits(hosts) as { problem: unknown }).problem).toBeNull()
  })

  it('is pending while any host is loading', () => {
    expect(assignDigits(roles(['pg-01', 'primary'], ['pg-02', 'loading']))).toEqual({ status: 'pending' })
  })

  it('reports no primary when all hosts are replicas or failed', () => {
    const a = assignDigits(roles(['pg-01', 'replica'], ['pg-02', 'failed']))
    expect(a).toEqual({ status: 'ready', digits: new Map([[2, 'pg-01']]), problem: { reason: 'no-primary' } })
  })

  it('leaves 1 empty with several primaries', () => {
    const a = assignDigits(roles(['pg-01', 'primary'], ['pg-02', 'primary'], ['pg-03', 'replica']))
    expect(a).toEqual({
      status: 'ready',
      digits: new Map([[2, 'pg-03']]),
      problem: { reason: 'many-primaries', hosts: ['pg-01', 'pg-02'] },
    })
  })

  it('does not number failed hosts', () => {
    expect(digitsOf(roles(['pg-01', 'primary'], ['pg-02', 'failed'], ['pg-03', 'replica']))).toEqual({
      1: 'pg-01',
      2: 'pg-03',
    })
  })

  it('numbers replicas up to 9 only', () => {
    const hosts = roles(
      ['pg-00', 'primary'],
      ...Array.from({ length: 10 }, (_, i): [string, HostRoleState] => [`r-${i}`, 'replica']),
    )
    const digits = digitsOf(hosts)
    expect(Object.keys(digits).map(Number)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9])
    expect(digits[9]).toBe('r-7')
    expect(Object.values(digits)).not.toContain('r-8')
  })

  it('follows hostItems order: display name, else host name', () => {
    const cluster: Cluster = {
      instances: [
        { host_name: 'c-host', display_name: 'a-alias' },
        { host_name: 'b-host' },
        { host_name: 'z-primary' },
      ],
    }
    const hosts = hostItems(cluster).map(h => ({
      host: h.value,
      state: (h.value === 'z-primary' ? 'primary' : 'replica') as HostRoleState,
    }))
    expect(digitsOf(hosts)).toEqual({ 1: 'z-primary', 2: 'c-host', 3: 'b-host' })
  })

  it('has nothing for a cluster without hosts', () => {
    expect(assignDigits([])).toEqual({ status: 'ready', digits: new Map(), problem: null })
  })
})

describe('hostForDigit', () => {
  it('returns the host, the pending state or the reason', () => {
    const many = assignDigits(roles(['a', 'primary'], ['b', 'primary'], ['c', 'replica']))
    expect(hostForDigit(many, 2)).toEqual({ host: 'c' })
    expect(hostForDigit(many, 1)).toEqual({ problem: { reason: 'many-primaries', hosts: ['a', 'b'] } })
    expect(hostForDigit(many, 5)).toEqual({ problem: null })
    expect(hostForDigit({ status: 'pending' }, 1)).toEqual({ pending: true })
  })
})
