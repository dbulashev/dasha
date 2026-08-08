import { defineStore } from 'pinia'

import { getInstanceInfo } from '@/api/gen/default/default'
import type { InstanceInfo } from '@/api/models'
import { assertOk } from '@/utils/api'

const CACHE_TTL_MS = 30 * 1000

// Persisted entries are dropped on hydrate once older than this. The cache only
// exists to paint a known role on the first frame after a reload, and the role
// gates menu items (Maintenance, Schema lint), so a value that outlives a
// failover by much is worse than a moment of skeleton: keep the bound short.
const PERSIST_TTL_MS = 5 * 60 * 1000

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
    failed: {} as Record<string, boolean>,
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
    // Last known info, ignoring the TTL: the TTL decides when to refetch, not
    // what to display — a role badge or a primary-only menu item must not
    // flicker while a refresh is pending.
    known(): (cluster: string, host: string) => InstanceInfo | null {
      return (cluster, host) => this.byHost[key(cluster, host)]?.info ?? null
    },
    isReplica(): (cluster: string, host: string) => boolean {
      return (cluster, host) => {
        const info = this.known(cluster, host)
        return info?.InRecovery === true
      }
    },
    role(): (cluster: string, host: string) => 'primary' | 'replica' | null {
      return (cluster, host) => {
        const info = this.known(cluster, host)
        if (!info) return null
        return info.InRecovery ? 'replica' : 'primary'
      }
    },
    // True while the role is not known yet and the host has not been ruled out:
    // a request is running, or none has been made since the last success. Lets
    // the UI show a placeholder instead of claiming the role is unknown.
    pending(): (cluster: string, host: string) => boolean {
      return (cluster, host) => {
        if (!cluster || !host) return false
        const k = key(cluster, host)
        if (this.byHost[k]) return false
        return this.inflight[k] !== undefined || !this.failed[k]
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
          delete this.failed[k]
          return info
        } catch {
          this.failed[k] = true
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
        const k = key(cluster, host)
        delete this.byHost[k]
        delete this.failed[k]
        return
      }
      this.byHost = {}
      this.failed = {}
    },
  },
  // Persist the cache so a reload paints the role straight away instead of
  // going through a loading state. inflight holds Promises and failed must not
  // outlive the session, so neither is persisted.
  persist: {
    storage: localStorage,
    pick: ['byHost'],
    afterHydrate(ctx) {
      const byHost = ctx.store.byHost as Record<string, CacheEntry>
      for (const k of Object.keys(byHost)) {
        if (Date.now() - byHost[k].fetchedAt > PERSIST_TTL_MS) delete byHost[k]
      }
    },
  },
})
