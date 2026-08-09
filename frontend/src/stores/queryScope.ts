import { defineStore } from 'pinia'

export type QueryScope = 'database' | 'instance'

// Which slice of pg_stat_statements the query pages show. The view is
// per-database while its contents are instance-wide, so both readings are
// legitimate; kept per cluster, like the other page settings.
export const useQueryScopeStore = defineStore('queryScope', {
  state: () => ({
    byCluster: {} as Record<string, QueryScope>,
  }),
  actions: {
    getScope(cluster: string): QueryScope {
      return this.byCluster[cluster] ?? 'database'
    },
    setScope(cluster: string, scope: QueryScope) {
      this.byCluster[cluster] = scope
    },
  },
  persist: {
    storage: localStorage,
  },
})
