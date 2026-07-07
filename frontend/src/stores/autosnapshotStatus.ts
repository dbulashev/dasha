import { defineStore } from 'pinia'

import { getAutosnapshotStatus } from '@/api/gen/default/default'
import type { AutoSnapshotStatus } from '@/api/models'

const CACHE_TTL_MS = 10 * 60 * 1000 // 10 minutes

export const useAutosnapshotStatusStore = defineStore('autosnapshotStatus', {
  state: () => ({
    available: false,
    enabled: false,
    cachedAt: null as number | null,
    loading: false,
  }),
  getters: {
    isCacheValid(): boolean {
      return this.cachedAt !== null && Date.now() - this.cachedAt < CACHE_TTL_MS
    },
  },
  actions: {
    async ensureLoaded() {
      if (this.isCacheValid || this.loading) return
      this.loading = true
      try {
        const res = await getAutosnapshotStatus()
        const body = res.data as AutoSnapshotStatus
        this.available = !!body?.Available
        this.enabled = !!body?.Enabled
        this.cachedAt = Date.now()
      } catch {
        // Transient failure: keep the last-known state (don't flip the menu off)
        // and invalidate the cache so the next ensureLoaded() retries. Flipping
        // available=false here while a prior cachedAt is still valid would wedge
        // the menu hidden until the cache expired.
        this.cachedAt = null
      } finally {
        this.loading = false
      }
    },
    invalidateCache() {
      this.cachedAt = null
    },
  },
  persist: {
    storage: localStorage,
    afterHydrate(ctx) {
      // Always re-validate on load: drop the cache so ensureLoaded() refetches
      // the real status. Persisted available/enabled still render instantly; the
      // fetch corrects them. This also un-wedges any stale persisted state.
      ctx.store.cachedAt = null
      ctx.store.loading = false
    },
  },
})
