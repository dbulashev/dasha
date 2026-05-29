import { defineStore } from 'pinia'

import { getInstanceInfo } from '@/api/gen/default/default'
import type { InstanceInfo } from '@/api/models'
import { assertOk } from '@/utils/api'

const CACHE_TTL_MS = 30 * 1000

interface CacheEntry {
  info: InstanceInfo
  fetchedAt: number
}

function key(cluster: string, host: string): string {
  return `${cluster}::${host}`
}

export const useInstanceInfoStore = defineStore('instanceInfo', {
  state: () => ({
    byHost: {} as Record<string, CacheEntry>,
    inflight: {} as Record<string, Promise<InstanceInfo | null>>,
  }),
  getters: {
    get(): (cluster: string, host: string) => InstanceInfo | null {
      return (cluster, host) => {
        const k = key(cluster, host)
        const entry = this.byHost[k]
        if (!entry) return null
        return Date.now() - entry.fetchedAt < CACHE_TTL_MS ? entry.info : null
      }
    },
    isReplica(): (cluster: string, host: string) => boolean {
      return (cluster, host) => {
        const info = this.get(cluster, host)
        return info?.InRecovery === true
      }
    },
  },
  actions: {
    async ensure(cluster: string, host: string): Promise<InstanceInfo | null> {
      if (!cluster || !host) return null

      const cached = this.get(cluster, host)
      if (cached) return cached

      const k = key(cluster, host)
      const existing = this.inflight[k] as Promise<InstanceInfo | null> | undefined
      if (existing) return existing

      const p = (async () => {
        try {
          const response = await getInstanceInfo({ cluster_name: cluster, instance: host })
          const info = assertOk<InstanceInfo>(response)
          this.byHost[k] = { info, fetchedAt: Date.now() }
          return info
        } catch {
          return null
        } finally {
          delete this.inflight[k]
        }
      })()

      this.inflight[k] = p
      return p
    },
    invalidate(cluster?: string, host?: string) {
      if (cluster && host) {
        delete this.byHost[key(cluster, host)]
        return
      }
      this.byHost = {}
    },
  },
})
