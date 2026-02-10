import { defineStore } from 'pinia'

import type { Cluster } from '@/api/models'

const CACHE_TTL_MS = 30 * 1000 // 30 seconds

export const useClustersStore = defineStore('clusters', {
  state: () => ({
    clusterList: null as Cluster[] | null,
    cachedAt: null as number | null,
  }),
  getters: {
    isCacheValid(): boolean {
      return this.cachedAt !== null && Date.now() - this.cachedAt < CACHE_TTL_MS
    },
  },
  actions: {
    setClusters(data: Cluster[]) {
      if (!data.length) return
      this.clusterList = data
      this.cachedAt = Date.now()
    },
    invalidateCache() {
      this.clusterList = null
      this.cachedAt = null
    },
  },
  persist: {
    storage: localStorage,
    afterHydrate(ctx) {
      const store = ctx.store
      if (store.cachedAt !== null && Date.now() - store.cachedAt > CACHE_TTL_MS) {
        store.clusterList = null
        store.cachedAt = null
      }
    },
  },
})
