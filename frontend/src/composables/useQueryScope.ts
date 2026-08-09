import { computed } from 'vue'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useQueryScopeStore, type QueryScope } from '@/stores/queryScope'

// Shared read/write access to the current/whole-instance switch of the query
// pages, so every section sends the same scope without threading props through.
export function useQueryScope() {
  const { clusterName, currentCluster } = useClusterInfo()
  const store = useQueryScopeStore()

  // One database is nothing to choose between, so the switch is hidden — and the
  // stored choice ignored, or a cluster left on "instance" would stay there with
  // no control to leave it.
  const hasScopeChoice = computed(() => (currentCluster.value?.databases?.length ?? 0) > 1)

  const scope = computed<QueryScope>({
    get: () => (hasScopeChoice.value && clusterName.value ? store.getScope(clusterName.value) : 'database'),
    set: (value) => {
      if (clusterName.value) store.setScope(clusterName.value, value)
    },
  })

  const isInstanceScope = computed(() => scope.value === 'instance')

  return { scope, isInstanceScope, hasScopeChoice }
}
