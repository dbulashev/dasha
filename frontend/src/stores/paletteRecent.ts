import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import type { Cluster } from '@/api/models'
import { useClustersStore } from '@/stores/clusters'
import { sectionBySlug, type SectionDef } from '@/utils/palette/sections'

export const RECENT_LIMIT = 10

export interface RecentEntry {
  cluster: string
  section: string
}

export interface LiveRecentEntry {
  cluster: string
  section: SectionDef
}

function isEntry(v: unknown): v is RecentEntry {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as RecentEntry).cluster === 'string' &&
    typeof (v as RecentEntry).section === 'string'
  )
}

/**
 * Entries that can still be shown: the section must exist (a route may have been
 * renamed), the cluster too once the cluster list is known. Without a list the
 * clusters cannot be checked, and the entries are kept.
 */
export function liveRecent(items: readonly RecentEntry[], clusters: readonly Cluster[] | null): LiveRecentEntry[] {
  const names = clusters ? new Set(clusters.map(c => c.name)) : null
  const out: LiveRecentEntry[] = []
  for (const item of items) {
    const section = sectionBySlug(item.section)
    if (section && (!names || names.has(item.cluster))) out.push({ cluster: item.cluster, section })
  }
  return out
}

// Navigation made from the palette only, newest first: the array order is the
// recency, so no timestamps are kept.
export const usePaletteRecentStore = defineStore(
  'paletteRecent',
  () => {
    const items = ref<RecentEntry[]>([])
    const clusterStore = useClustersStore()

    const entries = computed(() => liveRecent(items.value, clusterStore.clusterList))

    function record(cluster: string, section: string) {
      // Another tab may have recorded something since this one last read the
      // storage; writing without reading would drop it.
      usePaletteRecentStore().$hydrate()
      items.value = [
        { cluster, section },
        ...items.value.filter(i => i.cluster !== cluster || i.section !== section),
      ].slice(0, RECENT_LIMIT)
    }

    return { items, entries, record }
  },
  {
    persist: {
      storage: localStorage,
      pick: ['items'],
      afterHydrate(ctx) {
        const items: unknown = ctx.store.items
        ctx.store.items = Array.isArray(items) ? items.filter(isEntry).slice(0, RECENT_LIMIT) : []
      },
    },
  },
)
