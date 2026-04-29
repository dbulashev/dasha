import { defineStore } from 'pinia'

// Per-cluster state for the Active Queries section.
// Holds filters (IMP-3), refresh control (IMP-8), and the existing min-duration selector.
type FilterMode = 'like' | 'not_like'

interface ClusterState {
  minDuration: number
  queryFilter: string
  queryFilterMode: FilterMode
  username: string | null
  intervalSec: number // 1 | 5 | 10
}

const defaultState: ClusterState = {
  minDuration: 10,
  queryFilter: '',
  queryFilterMode: 'like',
  username: null,
  intervalSec: 5,
}

export const useActiveQueriesStore = defineStore('activeQueries', {
  state: () => ({
    byCluster: {} as Record<string, ClusterState>,
  }),
  actions: {
    get(cluster: string): ClusterState {
      return this.byCluster[cluster] ?? { ...defaultState }
    },
    patch(cluster: string, patch: Partial<ClusterState>) {
      const current = this.byCluster[cluster] ?? { ...defaultState }
      this.byCluster[cluster] = { ...current, ...patch }
    },
    getMinDuration(cluster: string): number {
      return this.byCluster[cluster]?.minDuration ?? defaultState.minDuration
    },
    setMinDuration(cluster: string, value: number) {
      this.patch(cluster, { minDuration: value })
    },
  },
  persist: {
    storage: localStorage,
  },
})
