import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useClustersStore } from '@/stores/clusters'

export function useClusterInfo() {
  const route = useRoute()
  const clusterStore = useClustersStore()

  const clusterName = computed(() => route.params.clustername ? String(route.params.clustername) : null)
  const databaseName = computed(() => route.query.db ? String(route.query.db) : null)
  const hostName = computed(() => route.query.host ? String(route.query.host) : null)

  const currentCluster = computed(() => 
    clusterStore.clusterList?.find(c => c.name === clusterName.value) ?? null
  )

  const currentHost = computed(() => 
    currentCluster.value?.instances?.find(i => i.host_name === hostName.value) ?? null
  )

  return {
    clusterName,
    databaseName,
    hostName,
    currentCluster,
    currentHost
  }
}