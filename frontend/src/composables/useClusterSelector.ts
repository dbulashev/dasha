import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useClustersStore } from '@/stores/clusters'
import { getClusters } from '@/api/gen/default/default'
import { getErrorMessage } from '@/utils/error'

export function useClusterSelector() {
  const clusterStore = useClustersStore()
  const route = useRoute()
  const router = useRouter()

  const loading = ref(false)
  const error = ref<string | null>(null)
  const initialized = ref(false)

  const selectedCluster = ref<string | null>(null)
  const selectedHost = ref<string | null>(null)
  const selectedDb = ref<string | null>(null)

  // Flag to prevent circular URL ↔ state updates
  const isSyncing = ref(false)

  // --- Options ---
  const clusterNames = computed(() =>
    (clusterStore.clusterList?.map(c => c.name).filter(Boolean) as string[] ?? []).sort(),
  )

  const hostOptions = computed(() => {
    const c = clusterStore.clusterList?.find(c => c.name === selectedCluster.value)
    return (c?.instances?.map(i => i.host_name).filter(Boolean) as string[] ?? []).sort()
  })

  const dbOptions = computed(() => {
    const c = clusterStore.clusterList?.find(c => c.name === selectedCluster.value)
    return [...(c?.databases ?? [])].sort()
  })

  // --- Helpers ---
  function resolveHost(desired: string | null, hosts: string[]): string | null {
    if (desired && hosts.includes(desired)) return desired
    return hosts[0] ?? null
  }

  function resolveDb(desired: string | null, dbs: string[]): string | null {
    if (desired && dbs.includes(desired)) return desired
    return dbs[0] ?? null
  }

  function pushToUrl() {
    if (!selectedCluster.value) return
    const targetHost = selectedHost.value
    const targetDb = selectedDb.value
    if (!targetHost && hostOptions.value.length > 0) return

    const { host: _h, db: _d, ...extraQuery } = route.query
    router.replace({
      name: route.name!,
      params: { clustername: selectedCluster.value },
      query: {
        ...(targetHost ? { host: targetHost } : {}),
        ...(targetDb ? { db: targetDb } : {}),
        ...extraQuery,
      },
    })
  }

  // --- Sync URL → state ---
  function syncStateFromUrl() {
    if (!clusterStore.clusterList?.length) return

    isSyncing.value = true

    const urlCluster = route.params.clustername ? String(route.params.clustername) : null
    const urlHost = route.query.host ? String(route.query.host) : null
    const urlDb = route.query.db ? String(route.query.db) : null

    const names = clusterNames.value

    if (urlCluster && names.includes(urlCluster)) {
      selectedCluster.value = urlCluster
    } else if (names.length > 0) {
      selectedCluster.value = names[0] ?? null
    }

    if (selectedCluster.value) {
      const cluster = clusterStore.clusterList?.find(c => c.name === selectedCluster.value)
      if (cluster) {
        const hosts = cluster.instances?.map(i => i.host_name).filter(Boolean) as string[] ?? []
        const dbs = cluster.databases ?? []
        selectedHost.value = resolveHost(urlHost, hosts)
        selectedDb.value = resolveDb(urlDb, dbs)
      }
    }

    isSyncing.value = false
  }

  // --- Load clusters ---
  async function loadClusters() {
    await router.isReady()

    if (clusterStore.isCacheValid && clusterStore.clusterList?.length) {
      syncStateFromUrl()
      await nextTick()
      initialized.value = true
      pushToUrl()
      return
    }

    loading.value = true
    try {
      const res = await getClusters()
      clusterStore.setClusters(res.data)
      syncStateFromUrl()
      await nextTick()
      initialized.value = true
      pushToUrl()
    } catch (err: unknown) {
      error.value = getErrorMessage(err)
      clusterStore.invalidateCache()
    } finally {
      loading.value = false
    }
  }

  onMounted(loadClusters)

  // --- Watchers ---
  // URL → state (external navigation)
  watch(
    () => [route.params.clustername, route.query.host, route.query.db],
    () => {
      if (!initialized.value || isSyncing.value) return
      syncStateFromUrl()
    },
    { deep: true },
  )

  // Cluster change by user → reset host/db
  watch(selectedCluster, (newCluster, oldCluster) => {
    if (!initialized.value || isSyncing.value || !newCluster) return
    if (newCluster === oldCluster) return

    const cluster = clusterStore.clusterList?.find(c => c.name === newCluster)
    if (!cluster) return

    const hosts = cluster.instances?.map(i => i.host_name).filter(Boolean) as string[] ?? []
    const dbs = cluster.databases ?? []

    isSyncing.value = true
    selectedHost.value = resolveHost(null, hosts)
    selectedDb.value = resolveDb(null, dbs)
    isSyncing.value = false

    pushToUrl()
  })

  // Host or DB change by user → update URL
  watch([selectedHost, selectedDb], () => {
    if (!initialized.value || isSyncing.value) return
    if (!selectedCluster.value) return
    pushToUrl()
  })

  return {
    loading,
    error,
    selectedCluster,
    selectedHost,
    selectedDb,
    clusterNames,
    hostOptions,
    dbOptions,
  }
}
