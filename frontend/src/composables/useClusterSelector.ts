import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useClustersStore } from '@/stores/clusters'
import { getClusters } from '@/api/gen/default/default'
import { assertOk, ApiError } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { useViewError } from '@/composables/useViewError'
import { useI18n } from 'vue-i18n'

export function useClusterSelector() {
  const clusterStore = useClustersStore()
  const route = useRoute()
  const router = useRouter()

  const { t } = useI18n()
  const { onError: setGlobalError, clearError } = useViewError()
  const loading = ref(false)
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

  function findSimilar(needle: string, haystack: string[], max = 3): string[] {
    const lower = needle.toLowerCase()
    // Substring match first
    const substring = haystack.filter(s => {
      const sl = s.toLowerCase()
      return sl.includes(lower) || lower.includes(sl)
    })
    if (substring.length) return substring.slice(0, max)
    // Fallback: common prefix (at least 2 chars)
    return haystack
      .filter(s => {
        const sl = s.toLowerCase()
        let i = 0
        while (i < sl.length && i < lower.length && sl[i] === lower[i]) i++
        return i >= 2
      })
      .slice(0, max)
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

  // --- Sync URL → state (returns false if URL points to unknown cluster/host) ---
  function syncStateFromUrl(): boolean {
    if (!clusterStore.clusterList?.length) return false

    isSyncing.value = true

    const urlCluster = route.params.clustername ? String(route.params.clustername) : null
    const urlHost = route.query.host ? String(route.query.host) : null
    const urlDb = route.query.db ? String(route.query.db) : null

    const names = clusterNames.value

    // Cluster not found → show 404
    if (urlCluster && !names.includes(urlCluster)) {
      isSyncing.value = false
      const similar = findSimilar(urlCluster, names)
      const hint = similar.length
        ? t('Did you mean: {list}?', { list: similar.join(', ') })
        : t('Available clusters: {list}', { list: names.slice(0, 5).join(', ') })
      setGlobalError(
        t('Cluster not found: {name}', { name: urlCluster }) + '. ' + hint,
        new ApiError(404, ''),
      )
      return false
    }

    selectedCluster.value = urlCluster ?? (names[0] ?? null)

    if (selectedCluster.value) {
      const cluster = clusterStore.clusterList?.find(c => c.name === selectedCluster.value)
      if (cluster) {
        const hosts = cluster.instances?.map(i => i.host_name).filter(Boolean) as string[] ?? []
        const dbs = cluster.databases ?? []

        // Host not found → show 404
        if (urlHost && !hosts.includes(urlHost)) {
          isSyncing.value = false
          const similar = findSimilar(urlHost, hosts)
          const hint = similar.length
            ? t('Did you mean: {list}?', { list: similar.join(', ') })
            : t('Available hosts: {list}', { list: hosts.slice(0, 5).join(', ') })
          setGlobalError(
            t('Host not found: {name}', { name: urlHost }) + '. ' + hint,
            new ApiError(404, ''),
          )
          return false
        }

        selectedHost.value = resolveHost(urlHost, hosts)
        selectedDb.value = resolveDb(urlDb, dbs)
      }
    }

    isSyncing.value = false
    clearError()
    return true
  }

  // --- Load clusters ---
  async function loadClusters() {
    await router.isReady()

    if (clusterStore.isCacheValid && clusterStore.clusterList?.length) {
      const ok = syncStateFromUrl()
      await nextTick()
      initialized.value = true
      if (ok) pushToUrl()
      return
    }

    loading.value = true
    try {
      const res = await getClusters()
      const data = assertOk(res) ?? []
      if (!data.length) {
        setGlobalError(t('No clusters available. Check backend configuration or wait for service discovery.'), new ApiError(503, ''))
        clusterStore.invalidateCache()
        return
      }
      clusterStore.setClusters(data)
      const ok = syncStateFromUrl()
      await nextTick()
      initialized.value = true
      if (ok) pushToUrl()
    } catch (err: unknown) {
      setGlobalError(getErrorMessage(err), err)
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
    selectedCluster,
    selectedHost,
    selectedDb,
    clusterNames,
    hostOptions,
    dbOptions,
  }
}
