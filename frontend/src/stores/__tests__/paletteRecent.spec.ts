import { beforeEach, describe, expect, it } from 'vitest'
import { createApp, nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import piniaPluginPersistedstate from 'pinia-plugin-persistedstate'

import { useClustersStore } from '@/stores/clusters'
import { liveRecent, RECENT_LIMIT, usePaletteRecentStore } from '@/stores/paletteRecent'
import { sectionBySlug } from '@/utils/palette/sections'

const KEY = 'paletteRecent'

// Pinia applies plugins only once installed in an app.
function freshStore() {
  const pinia = createPinia()
  pinia.use(piniaPluginPersistedstate)
  createApp({}).use(pinia)
  setActivePinia(pinia)
  return usePaletteRecentStore()
}

beforeEach(() => {
  localStorage.clear()
})

describe('record', () => {
  it('puts the newest pair first and lifts a repeated one without a duplicate', () => {
    const store = freshStore()
    store.record('a', 'locks')
    store.record('b', 'main')
    store.record('a', 'locks')
    expect(store.items).toEqual([
      { cluster: 'a', section: 'locks' },
      { cluster: 'b', section: 'main' },
    ])
  })

  it('keeps the same cluster with another section as a separate entry', () => {
    const store = freshStore()
    store.record('a', 'locks')
    store.record('a', 'io')
    expect(store.items).toHaveLength(2)
  })

  it(`keeps at most ${RECENT_LIMIT} entries`, () => {
    const store = freshStore()
    for (let i = 0; i < RECENT_LIMIT + 3; i++) store.record(`c${i}`, 'main')
    expect(store.items).toHaveLength(RECENT_LIMIT)
    expect(store.items[0]).toEqual({ cluster: `c${RECENT_LIMIT + 2}`, section: 'main' })
  })

  it('persists to localStorage under the store id', async () => {
    freshStore().record('a', 'locks')
    await nextTick()
    expect(JSON.parse(localStorage.getItem(KEY)!)).toEqual({ items: [{ cluster: 'a', section: 'locks' }] })
  })

  it('keeps what another tab wrote after this one loaded', async () => {
    const store = freshStore()
    store.record('a', 'locks')
    await nextTick()
    localStorage.setItem(
      KEY,
      JSON.stringify({ items: [{ cluster: 'b', section: 'io' }, { cluster: 'a', section: 'locks' }] }),
    )
    store.record('c', 'main')
    expect(store.items).toEqual([
      { cluster: 'c', section: 'main' },
      { cluster: 'b', section: 'io' },
      { cluster: 'a', section: 'locks' },
    ])
  })
})

describe('hydration', () => {
  it('restores the saved entries', () => {
    localStorage.setItem(KEY, JSON.stringify({ items: [{ cluster: 'a', section: 'locks' }] }))
    expect(freshStore().items).toEqual([{ cluster: 'a', section: 'locks' }])
  })

  it('drops entries of a wrong shape', () => {
    localStorage.setItem(
      KEY,
      JSON.stringify({ items: [{ cluster: 'a', section: 'locks' }, { cluster: 1, section: 'x' }, 'junk', null, { cluster: 'b' }] }),
    )
    expect(freshStore().items).toEqual([{ cluster: 'a', section: 'locks' }])
  })

  it('survives a value that is not a list', () => {
    localStorage.setItem(KEY, JSON.stringify({ items: 'nope' }))
    expect(freshStore().items).toEqual([])
  })
})

describe('entries', () => {
  it('hides unknown clusters and sections', () => {
    const store = freshStore()
    useClustersStore().setClusters([{ name: 'a' }])
    store.record('a', 'renamed-route')
    store.record('gone', 'locks')
    store.record('a', 'locks')
    expect(store.entries).toEqual([{ cluster: 'a', section: sectionBySlug('locks') }])
  })

  it('keeps entries while the cluster list is unknown', () => {
    expect(liveRecent([{ cluster: 'x', section: 'io' }, { cluster: 'x', section: 'nope' }], null)).toEqual([
      { cluster: 'x', section: sectionBySlug('io') },
    ])
  })
})
