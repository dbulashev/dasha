import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('@/api/gen/default/default', () => ({}))

import { AuthInfoMode, type InstanceInfo } from '@/api/models'
import { useSectionVisibility } from '@/composables/useSectionVisibility'
import { useAuthStore } from '@/stores/auth'
import { useAutosnapshotStatusStore } from '@/stores/autosnapshotStatus'
import { useInstanceInfoStore } from '@/stores/instanceInfo'
import { useSnapshotsStatusStore } from '@/stores/snapshotsStatus'
import { sectionBySlug } from '@/utils/palette/sections'

const cluster = ref('c')
const host = ref('h')

function seed(c: string, h: string, info: InstanceInfo) {
  useInstanceInfoStore().byHost[`${c}::${h}`] = { info, fetchedAt: Date.now() }
}

beforeEach(() => {
  setActivePinia(createPinia())
  cluster.value = 'c'
  host.value = 'h'
})

describe('menu flags of the current host', () => {
  it('hide version-gated items until the version is known', () => {
    const v = useSectionVisibility({ cluster, host })
    expect(v.menuVisible('activity')).toBe(false)
    expect(v.menuVisible('io')).toBe(false)

    seed('c', 'h', { VersionNum: 150000, InRecovery: false })
    expect(v.menuVisible('activity')).toBe(true)
    expect(v.menuVisible('io')).toBe(false)
    expect(v.isReplica.value).toBe(false)
  })

  it('follow the host in the URL', () => {
    const v = useSectionVisibility({ cluster, host })
    seed('c', 'r', { VersionNum: 170000, InRecovery: true })
    expect(v.isReplica.value).toBe(false)
    host.value = 'r'
    expect(v.isReplica.value).toBe(true)
    expect(v.menuVisible('io')).toBe(true)
    expect(v.menuVisible('maintenance')).toBe(false)
    expect(v.menuVisible('schema-lint')).toBe(false)
  })

  it('show Query Compare once snapshots are available', () => {
    const v = useSectionVisibility({ cluster, host })
    expect(v.menuVisible('query-compare')).toBe(false)
    useSnapshotsStatusStore().available = true
    expect(v.menuVisible('query-compare')).toBe(true)
  })

  it('show auto snapshots to an admin while disabled, to others only when enabled', () => {
    const v = useSectionVisibility({ cluster, host })
    const auth = useAuthStore()
    const status = useAutosnapshotStatusStore()
    status.available = true
    auth.mode = AuthInfoMode.oidc
    auth.user = { name: 'u', email: 'u@example.com', role: 'viewer' }
    expect(v.autosnapshotVisible.value).toBe(false)
    status.enabled = true
    expect(v.autosnapshotVisible.value).toBe(true)
    status.enabled = false
    auth.user = { ...auth.user, role: 'admin' }
    expect(v.autosnapshotVisible.value).toBe(true)
    expect(v.isAdmin.value).toBe(true)
  })
})

describe('visible', () => {
  const s = (slug: string) => sectionBySlug(slug)!

  it('checks logs per cluster', () => {
    const v = useSectionVisibility({ cluster, host })
    expect(v.visible(s('logs'), { cluster: { name: 'a', supports_logs: true }, host: null, current: false })).toBe(true)
    expect(v.visible(s('logs'), { cluster: { name: 'b' }, host: null, current: false })).toBe(false)
  })

  it('offers version-gated and primary-only sections of another cluster while unknown', () => {
    const v = useSectionVisibility({ cluster, host })
    const target = { cluster: { name: 'o', instances: [{ host_name: 'oh' }] }, host: null, current: false }
    expect(v.visible(s('io'), target)).toBe(true)
    expect(v.visible(s('maintenance'), target)).toBe(true)

    seed('o', 'oh', { VersionNum: 150000, InRecovery: true })
    expect(v.visible(s('io'), target)).toBe(false)
    expect(v.visible(s('maintenance'), target)).toBe(false)
  })

  it('judges another cluster by its default host, not by the host passed in', () => {
    const v = useSectionVisibility({ cluster, host })
    const other = { name: 'o', instances: [{ host_name: 'first' }, { host_name: 'second' }] }
    seed('o', 'first', { VersionNum: 130000, InRecovery: true })
    seed('o', 'second', { VersionNum: 170000, InRecovery: false })
    const target = { cluster: other, host: 'second', current: false }
    expect(v.visible(s('io'), target)).toBe(false)
    expect(v.visible(s('maintenance'), target)).toBe(false)
  })

  it('looks clusters up by name for the search, the current one by the URL host', () => {
    const v = useSectionVisibility({ cluster, host })
    seed('c', 'h', { VersionNum: 170000, InRecovery: true })
    seed('o', 'oh', { VersionNum: 130000, InRecovery: false })
    const byName = v.visibleByName([
      { name: 'c', instances: [{ host_name: 'x' }, { host_name: 'h' }] },
      { name: 'o', instances: [{ host_name: 'oh' }], supports_logs: true },
    ])
    expect(byName('c')(s('io'))).toBe(true)
    expect(byName('c')(s('maintenance'))).toBe(false)
    expect(byName('o')(s('io'))).toBe(false)
    expect(byName('o')(s('logs'))).toBe(true)
    expect(byName('missing')(s('io'))).toBe(true)
  })

  it('hides version-gated sections of the current host while unknown, as the menu does', () => {
    const v = useSectionVisibility({ cluster, host })
    expect(v.visible(s('activity'), { cluster: { name: 'c' }, host: 'h', current: true })).toBe(false)
  })
})
