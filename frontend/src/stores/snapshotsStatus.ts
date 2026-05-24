import { defineStore } from 'pinia'

import { getSnapshotsStatus } from '@/api/gen/default/default'
import type { SnapshotStatus } from '@/api/models'

const CACHE_TTL_MS = 10 * 60 * 1000 // 10 minutes

export const useSnapshotsStatusStore = defineStore('snapshotsStatus', {
  state: () => ({
    available: false,
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
        const res = await getSnapshotsStatus()
        const body = res.data as SnapshotStatus
        this.available = !!body?.Available
        this.cachedAt = Date.now()
      } catch {
        this.available = false
        this.cachedAt = Date.now()
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
        store.cachedAt = null
      }
      store.loading = false
    },
  },
})
