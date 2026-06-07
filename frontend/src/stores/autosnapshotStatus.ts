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
        // Don't cache failures — leave cachedAt unset so ensureLoaded() retries.
        this.available = false
        this.enabled = false
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
      const store = ctx.store
      if (store.cachedAt !== null && Date.now() - store.cachedAt > CACHE_TTL_MS) {
        store.available = false
        store.enabled = false
        store.cachedAt = null
      }
      store.loading = false
    },
  },
})
