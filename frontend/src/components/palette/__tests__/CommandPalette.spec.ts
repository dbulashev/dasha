import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import piniaPluginPersistedstate from 'pinia-plugin-persistedstate'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'

vi.mock('@/api/gen/default/default', () => ({
  getClusters: vi.fn(() => new Promise(() => {})),
  getInstanceInfo: vi.fn(() => new Promise(() => {})),
}))

import enUS from '@/locales/en_US.json'
import CommandPalette from '@/components/palette/CommandPalette.vue'
import { useClustersStore } from '@/stores/clusters'
import { useInstanceInfoStore } from '@/stores/instanceInfo'
import { usePaletteRecentStore } from '@/stores/paletteRecent'
import { SECTIONS } from '@/utils/palette/sections'

globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
} as unknown as typeof ResizeObserver
// VOverlay positions itself against the visual viewport, which jsdom lacks.
globalThis.visualViewport ??= Object.assign(new EventTarget(), {
  width: 1024,
  height: 768,
  offsetLeft: 0,
  offsetTop: 0,
  pageLeft: 0,
  pageTop: 0,
  scale: 1,
}) as unknown as VisualViewport

const Empty = defineComponent({ render: () => h('div') })

function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      ...SECTIONS.map(s => ({ path: `/${s.slug}/:clustername?`, name: s.routeName, component: Empty })),
      { path: '/fleet/:clustername?', name: 'fleet', component: Empty },
    ],
  })
}

let wrapper: VueWrapper | null = null
let router: Router

async function mountPalette(path = '/locks/c1?host=pg-02&db=app') {
  const pinia = createPinia()
  pinia.use(piniaPluginPersistedstate)
  setActivePinia(pinia)
  const clusterStore = useClustersStore()
  clusterStore.setClusters([
    {
      name: 'c1',
      instances: [{ host_name: 'pg-01' }, { host_name: 'pg-02' }, { host_name: 'pg-03' }],
      databases: ['app'],
    },
    {
      name: 'c2',
      instances: [{ host_name: 'rc1a-aaaaa.mdb.yandex' }, { host_name: 'rc1b-bbbbb.mdb.yandex' }],
      databases: ['billing'],
    },
  ])
  const info = useInstanceInfoStore()
  info.byHost['c1::pg-01'] = { info: { InRecovery: false, VersionNum: 170000 }, fetchedAt: Date.now() }
  info.byHost['c1::pg-02'] = { info: { InRecovery: true, VersionNum: 170000 }, fetchedAt: Date.now() }
  info.byHost['c1::pg-03'] = { info: { InRecovery: true, VersionNum: 170000 }, fetchedAt: Date.now() }
  info.byHost['c2::rc1b-bbbbb.mdb.yandex'] = { info: { InRecovery: false, VersionNum: 170000 }, fetchedAt: Date.now() }

  router = makeRouter()
  const open = ref(false)
  const Host = defineComponent({
    render: () => h(CommandPalette, { modelValue: open.value, 'onUpdate:modelValue': (v: boolean) => (open.value = v) }),
  })
  wrapper = mount(Host, {
    attachTo: document.body,
    global: {
      plugins: [
        pinia,
        router,
        createVuetify({ components, directives }),
        createI18n({ legacy: false, locale: 'en_US', messages: { en_US: enUS } }),
      ],
    },
  })
  // Installing the router starts its own first navigation; a push made before
  // it settles is cancelled by it.
  await router.isReady()
  await router.push(path)
  // The dialog's back-button guard delays navigations by a macrotask; what the
  // palette asked for is what matters here.
  const push = vi.spyOn(router, 'push').mockResolvedValue(undefined)
  open.value = true
  await flushPromises()
  return { open, pushed: () => push.mock.calls.map(([to]) => router.resolve(to).fullPath) }
}

function input(): HTMLInputElement {
  return document.querySelector('input[role="combobox"]') as HTMLInputElement
}

function key(k: string, init: KeyboardEventInit = {}): KeyboardEvent {
  const e = new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...init })
  input().dispatchEvent(e)
  return e
}

async function type(text: string) {
  input().value = text
  input().dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}

const active = () => input().getAttribute('aria-activedescendant')
const optionTitle = (id: string | null) =>
  document.getElementById(id ?? '')?.querySelector('.v-list-item-title')?.textContent?.trim()
const optionSubtitle = (id: string | null) =>
  document.getElementById(id ?? '')?.querySelector('.v-list-item-subtitle')?.textContent?.trim()

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
})

describe('CommandPalette', () => {
  it('takes a digit in an empty field as a jump, not as text', async () => {
    const { open, pushed } = await mountPalette()
    const e = key('1')
    expect(e.defaultPrevented).toBe(true)
    expect(input().value).toBe('')
    await flushPromises()
    expect(pushed()).toEqual(['/locks/c1?host=pg-01&db=app'])
    expect(open.value).toBe(false)
  })

  it('lets a digit into a field that already has text', async () => {
    const { pushed } = await mountPalette()
    await type('db')
    const e = key('1')
    expect(e.defaultPrevented).toBe(false)
    expect(pushed()).toEqual([])
  })

  it('ignores a digit with a modifier', async () => {
    await mountPalette()
    expect(key('1', { altKey: true }).defaultPrevented).toBe(false)
  })

  it('moves the selection around in a circle', async () => {
    await mountPalette()
    await type('pg')
    const rows = document.querySelectorAll('[role="option"]')
    expect(rows).toHaveLength(3)
    // pg-02 is the current host, so pg-01 is selected first.
    expect(optionTitle(active())).toBe('pg-01')
    key('ArrowUp')
    await flushPromises()
    expect(optionTitle(active())).toContain('pg-03')
    key('ArrowDown')
    await flushPromises()
    expect(optionTitle(active())).toBe('pg-01')
  })

  it('skips the current place in the initial selection', async () => {
    await mountPalette('/locks/c1?host=pg-01&db=app')
    await type('pg')
    expect(active()).toBe('palette-opt-1')
    expect(optionTitle(active())).toBe('pg-02')
  })

  it('goes to the selected row with Enter and remembers it', async () => {
    const { pushed } = await mountPalette()
    await type('repl')
    key('Enter')
    await flushPromises()
    expect(pushed()).toEqual(['/replication/c1?host=pg-02&db=app'])
    expect(usePaletteRecentStore().items).toEqual([{ cluster: 'c1', section: 'replication' }])
  })

  it('lists the hosts with their digits when the field is empty', async () => {
    await mountPalette()
    const rows = [...document.querySelectorAll('[role="option"]')]
    expect(rows.map(r => r.querySelector('.v-list-item-title')?.textContent?.trim())).toEqual([
      'pg-01',
      expect.stringContaining('pg-02'),
      'pg-03',
    ])
    expect(rows.map(r => r.querySelector('kbd')?.textContent)).toEqual(['1', '2', '3'])
  })

  it('shows no digits in the search results, where they do not work', async () => {
    await mountPalette()
    await type('pg')
    const kbds = [...document.querySelectorAll('[role="option"] kbd')].map(k => k.textContent)
    expect(kbds).toEqual(['↵'])
  })

  it('treats a field of spaces as empty for digits', async () => {
    const { pushed } = await mountPalette()
    await type('  ')
    expect(key('1').defaultPrevented).toBe(true)
    await flushPromises()
    expect(pushed()).toEqual(['/locks/c1?host=pg-01&db=app'])
  })

  it('keeps the list out of the tab order', async () => {
    await mountPalette()
    expect(document.getElementById('palette-list')?.getAttribute('tabindex')).toBe('-1')
  })

  describe('hosts of other clusters', () => {
    it('opens a pasted host in its cluster, in the same section', async () => {
      const { pushed } = await mountPalette()
      await type('rc1a-aaaaa.mdb.yandex')
      expect(optionTitle(active())).toBe('rc1a-aaaaa.mdb.yandex')
      expect(optionSubtitle(active())).toBe('c2 · → Locks')
      key('Enter')
      await flushPromises()
      expect(pushed()).toEqual(['/locks/c2?host=rc1a-aaaaa.mdb.yandex&db=billing'])
      expect(usePaletteRecentStore().items).toEqual([{ cluster: 'c2', section: 'locks' }])
    })

    it('finds a host by a substring of its name', async () => {
      await mountPalette()
      await type('aaaaa')
      expect(optionTitle(active())).toBe('rc1a-aaaaa.mdb.yandex')
    })

    it('opens the summary from a primary-only section when the role is not known', async () => {
      const { pushed } = await mountPalette('/maintenance/c1?host=pg-01&db=app')
      await type('aaaaa')
      expect(optionSubtitle(active())).toContain('unavailable')
      key('Enter')
      await flushPromises()
      expect(pushed()).toEqual(['/main/c2?host=rc1a-aaaaa.mdb.yandex&db=billing'])
    })

    it('stays in a primary-only section on a host known to be the primary', async () => {
      const { pushed } = await mountPalette('/maintenance/c1?host=pg-01&db=app')
      await type('bbbbb')
      expect(optionSubtitle(active())).toBe('c2 · Primary · → Maintenance')
      key('Enter')
      await flushPromises()
      expect(pushed()).toEqual(['/maintenance/c2?host=rc1b-bbbbb.mdb.yandex&db=billing'])
    })

    it('searches the hosts of every cluster on Fleet', async () => {
      const { pushed } = await mountPalette('/fleet/c1')
      await type('pg-03')
      expect(optionSubtitle(active())).toContain('c1')
      key('Enter')
      await flushPromises()
      expect(pushed()).toEqual(['/main/c1?host=pg-03&db=app'])
    })
  })
})
