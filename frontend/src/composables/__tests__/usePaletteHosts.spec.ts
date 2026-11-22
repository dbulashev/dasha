import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'

import type { InstanceInfo } from '@/api/models'

interface Call {
  host: string
  resolve: (info: InstanceInfo | null) => void
}
const calls: Call[] = []

vi.mock('@/api/gen/default/default', () => ({
  getInstanceInfo: (params: { instance: string }) =>
    new Promise(resolve => {
      calls.push({
        host: params.instance,
        resolve: info => resolve(info ? { status: 200, data: info } : { status: 500, data: { message: 'down' } }),
      })
    }),
}))

import { DIGIT_WAIT_MS, usePaletteHosts } from '@/composables/usePaletteHosts'
import { useInstanceInfoStore } from '@/stores/instanceInfo'
import type { HostItem } from '@/utils/clusters'

const PRIMARY: InstanceInfo = { InRecovery: false, VersionNum: 170000 }
const REPLICA: InstanceInfo = { InRecovery: true, VersionNum: 170000 }

function answer(host: string, info: InstanceInfo | null) {
  const i = calls.findIndex(c => c.host === host)
  calls.splice(i, 1)[0]!.resolve(info)
}

function setup(hostNames: string[]) {
  const onDigit = vi.fn()
  const scope = effectScope()
  const hosts = ref<HostItem[]>(hostNames.map(h => ({ value: h, title: h })))
  const api = scope.run(() => usePaletteHosts({ cluster: ref('c'), hosts, onDigit }))!
  return { api, onDigit, scope }
}

beforeEach(() => {
  setActivePinia(createPinia())
  calls.length = 0
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('usePaletteHosts', () => {
  it('waits for the roles and then goes to the primary', async () => {
    const { api, onDigit } = setup(['pg-01', 'pg-02'])
    api.start()
    api.press(1)
    expect(api.notice.value).toEqual({ kind: 'waiting', digit: 1 })
    expect(onDigit).not.toHaveBeenCalled()

    answer('pg-02', PRIMARY)
    answer('pg-01', REPLICA)
    await flushPromises()
    expect(onDigit).toHaveBeenCalledWith('pg-02')
    expect(api.pendingDigit.value).toBeNull()
  })

  it('waits for a replica digit too', async () => {
    const { api, onDigit } = setup(['pg-01', 'pg-02', 'pg-03'])
    api.start()
    api.press(3)
    answer('pg-01', PRIMARY)
    answer('pg-02', REPLICA)
    answer('pg-03', REPLICA)
    await flushPromises()
    expect(onDigit).toHaveBeenCalledWith('pg-03')
  })

  it('gives no digit by a role remembered past the TTL', async () => {
    const { api, onDigit } = setup(['pg-01', 'pg-02'])
    const store = useInstanceInfoStore()
    const old = Date.now() - 60_000
    store.byHost['c::pg-01'] = { info: PRIMARY, fetchedAt: old }
    store.byHost['c::pg-02'] = { info: REPLICA, fetchedAt: old }
    api.start()
    expect(api.assignment.value.status).toBe('pending')
    api.press(1)
    expect(onDigit).not.toHaveBeenCalled()
    answer('pg-01', REPLICA)
    answer('pg-02', PRIMARY)
    await flushPromises()
    expect(onDigit).toHaveBeenCalledWith('pg-02')
  })

  it('uses fresh roles from the store without waiting', async () => {
    const { api, onDigit } = setup(['pg-01', 'pg-02'])
    const store = useInstanceInfoStore()
    store.byHost['c::pg-01'] = { info: REPLICA, fetchedAt: Date.now() }
    store.byHost['c::pg-02'] = { info: PRIMARY, fetchedAt: Date.now() }
    api.start()
    expect(calls).toEqual([])
    api.press(1)
    expect(onDigit).toHaveBeenCalledWith('pg-02')
  })

  it('stops waiting after the limit without marking the store', async () => {
    const { api, onDigit } = setup(['pg-01', 'pg-02'])
    api.start()
    api.press(1)
    answer('pg-02', REPLICA)
    await flushPromises()
    vi.advanceTimersByTime(DIGIT_WAIT_MS)
    await nextTick()
    expect(onDigit).not.toHaveBeenCalled()
    expect(api.notice.value).toEqual({ kind: 'problem', digit: 1, problem: { reason: 'no-primary' } })
    expect(api.stateOf('pg-01')).toBe('failed')

    const store = useInstanceInfoStore()
    expect(store.failed['c::pg-01']).toBeUndefined()
    expect(store.inflight['c::pg-01']).toBeDefined()

    // The late answer still lands in the store, but the digit is no longer awaited.
    answer('pg-01', PRIMARY)
    await flushPromises()
    expect(store.known('c', 'pg-01')).toEqual(PRIMARY)
    expect(onDigit).not.toHaveBeenCalled()
  })

  it('is cancelled by typing', async () => {
    const { api, onDigit } = setup(['pg-01', 'pg-02'])
    api.start()
    api.press(1)
    api.cancelPending()
    expect(api.notice.value).toBeNull()
    answer('pg-01', PRIMARY)
    answer('pg-02', REPLICA)
    await flushPromises()
    expect(onDigit).not.toHaveBeenCalled()
  })

  it('is cancelled by closing, and a late answer does not navigate', async () => {
    const { api, onDigit } = setup(['pg-01', 'pg-02'])
    api.start()
    api.press(1)
    api.stop()
    answer('pg-01', PRIMARY)
    answer('pg-02', REPLICA)
    await flushPromises()
    vi.advanceTimersByTime(DIGIT_WAIT_MS)
    expect(onDigit).not.toHaveBeenCalled()
    expect(api.pendingDigit.value).toBeNull()
  })

  it('reports a failed host as unknown and gives it no digit', async () => {
    const { api } = setup(['pg-01', 'pg-02', 'pg-03'])
    api.start()
    answer('pg-01', PRIMARY)
    answer('pg-02', null)
    answer('pg-03', REPLICA)
    await flushPromises()
    expect(api.stateOf('pg-02')).toBe('failed')
    expect([...api.digitOf.value]).toEqual([
      ['pg-01', 1],
      ['pg-03', 2],
    ])
    expect(api.loading.value).toBe(false)
  })

  it('says why a digit leads nowhere', async () => {
    const { api, onDigit } = setup(['pg-01', 'pg-02'])
    api.start()
    answer('pg-01', PRIMARY)
    answer('pg-02', PRIMARY)
    await flushPromises()
    expect(api.problem.value).toEqual({ reason: 'many-primaries', hosts: ['pg-01', 'pg-02'] })
    api.press(1)
    expect(api.notice.value?.kind).toBe('problem')
    api.press(2)
    expect(api.notice.value).toEqual({ kind: 'no-host', digit: 2 })
    expect(onDigit).not.toHaveBeenCalled()
  })
})
