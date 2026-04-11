import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useClustersStore } from '@/stores/clusters'

export function useClusterInfo() {
  const route = useRoute()
  const clusterStore = useClustersStore()

  const currentCluster = computed(() => {
    const name = route.params.clustername ? String(route.params.clustername) : null
    if (!name) return null
    return clusterStore.clusterList?.find(c => c.name === name) ?? null
  })

  const currentHost = computed(() => {
    const host = route.query.host ? String(route.query.host) : null
    if (!host || !currentCluster.value) return null
    return currentCluster.value.instances?.find(i => i.host_name === host) ?? null
  })

  // Return null if cluster/host not found in store — prevents API calls with invalid params
  const clusterName = computed(() => currentCluster.value?.name ?? null)
  const hostName = computed(() => {
    const urlHost = route.query.host ? String(route.query.host) : null
    if (!urlHost) return null
    // Only return host if it exists in the cluster
    return currentHost.value?.host_name ?? null
  })
  const databaseName = computed(() => route.query.db ? String(route.query.db) : null)

  return {
    clusterName,
    databaseName,
    hostName,
    currentCluster,
    currentHost
  }
}