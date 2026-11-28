import { beforeEach, describe, expect, it, vi } from 'vitest'
import { computed } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { flushPromises, mount } from '@vue/test-utils'

import enUS from '@/locales/en_US.json'

const getLockStatsHistory = vi.fn()
const getSnapshotsStatus = vi.fn()
const getInstanceInfo = vi.fn()

vi.mock('@/api/gen/default/default', () => ({
  getLockStatsHistory: (...args: unknown[]) => getLockStatsHistory(...args),
  getSnapshotsStatus: (...args: unknown[]) => getSnapshotsStatus(...args),
  getInstanceInfo: (...args: unknown[]) => getInstanceInfo(...args),
}))

vi.mock('@/composables/useClusterInfo', () => ({
  useClusterInfo: () => ({
    clusterName: computed(() => 'c1'),
    databaseName: computed(() => 'app'),
    hostName: computed(() => 'host1'),
  }),
}))

const onError = vi.fn()
vi.mock('@/composables/useViewError', () => ({
  useViewError: () => ({ onError }),
}))

vi.mock('vue-chartjs', () => ({ Line: { template: '<div class="chart-stub" />' } }))

import LockStatsSection from '@/components/locks/LockStatsSection.vue'

globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
} as unknown as typeof ResizeObserver

const vuetify = createVuetify({ components, directives })
const i18n = createI18n({ legacy: false, locale: 'en_US', messages: { en_US: enUS } })

const emptyHistory = {
  meta: {
    instance: 'host1',
    version_changed: false,
    deadlock_timeout_ms: 1000,
    deadlock_timeout_changed: false,
    max_locks_per_transaction: 64,
    max_locks_per_transaction_changed: false,
  },
  series: [],
}

function setup(versionNum: number, storage: boolean) {
  getInstanceInfo.mockResolvedValue({ status: 200, data: { InRecovery: false, VersionNum: versionNum } })
  getSnapshotsStatus.mockResolvedValue({ status: 200, data: { Available: storage } })
}

async function render() {
  const wrapper = mount(LockStatsSection, { global: { plugins: [vuetify, i18n] } })
  await flushPromises()
  return wrapper
}

describe('LockStatsSection', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('explains the missing version on PG 18 and never calls the API', async () => {
    setup(180004, true)
    const wrapper = await render()

    expect(wrapper.text()).toContain('PostgreSQL 19')
    expect(getLockStatsHistory).not.toHaveBeenCalled()
  })

  it('explains the missing snapshot storage', async () => {
    setup(190000, false)
    const wrapper = await render()

    expect(wrapper.text()).toContain('Snapshot storage is not configured')
    expect(getLockStatsHistory).not.toHaveBeenCalled()
  })

  it('takes the reason from a 501', async () => {
    setup(190000, true)
    getLockStatsHistory.mockResolvedValue({ status: 501, data: { reason: 'unsupported_version' } })
    const wrapper = await render()

    expect(wrapper.text()).toContain('PostgreSQL 19')
    expect(onError).not.toHaveBeenCalled()
  })

  it('explains a missing privilege', async () => {
    setup(190000, true)
    getLockStatsHistory.mockResolvedValue({ status: 501, data: { reason: 'no_privilege' } })
    const wrapper = await render()

    expect(wrapper.text()).toContain('Not enough privileges')
  })

  it('loads when the instance info cannot be fetched', async () => {
    setup(190000, true)
    getInstanceInfo.mockResolvedValue({ status: 500, data: { message: 'boom' } })
    getLockStatsHistory.mockResolvedValue({ status: 200, data: emptyHistory })
    const wrapper = await render()

    expect(getLockStatsHistory).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('No snapshots in this period yet')
  })

  it('keeps a failed request inside the section', async () => {
    setup(190000, true)
    getLockStatsHistory.mockResolvedValue({ status: 500, data: { message: 'storage is down' } })
    const wrapper = await render()

    expect(wrapper.text()).toContain('storage is down')
    expect(onError).not.toHaveBeenCalled()
  })

  it('loads the series and the window totals on PG 19', async () => {
    setup(190000, true)
    getLockStatsHistory.mockResolvedValue({ status: 200, data: emptyHistory })
    const wrapper = await render()

    expect(getLockStatsHistory).toHaveBeenCalledTimes(2)
    expect(getLockStatsHistory.mock.calls.map((c) => (c[0] as { points: number }).points).sort()).toEqual([1, 200])
    expect(wrapper.text()).toContain('deadlock_timeout')
    expect(wrapper.text()).toContain('No snapshots in this period yet')
  })
})
