import { computed, ref, watch } from 'vue'
import { getQueryStatsStatus } from '@/api/gen/default/default'
import type { QueryStatsStatus } from '@/api/models'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

// Fallback for labels that must name an extension even before the status lands.
export const DEFAULT_STATS_SOURCE = 'pg_stat_statements'

// Matches the backend re-check window, so CREATE EXTENSION shows up without a reload.
const cacheTTL = 60_000

// Shared process-wide: one connection has one source, whoever asks.
const cache = new Map<string, { source: string; expires: number }>()
const inFlight = new Map<string, Promise<string>>()

function sourceKey(cluster: string, instance: string, database: string): string {
  return `${cluster} ${instance} ${database}`
}

function remember(key: string, source: string) {
  const now = Date.now()
  for (const [k, entry] of cache) {
    if (entry.expires <= now) cache.delete(k)
  }
  cache.set(key, { source, expires: now + cacheTTL })
}

function fetchSource(cluster: string, instance: string, database: string): Promise<string> {
  const key = sourceKey(cluster, instance, database)

  const cached = cache.get(key)
  if (cached && cached.expires > Date.now()) return Promise.resolve(cached.source)

  const pending = inFlight.get(key)
  if (pending) return pending

  const request = getQueryStatsStatus({ cluster_name: cluster, instance, database })
    .then((response) => {
      const status = assertOk<QueryStatsStatus>(response)
      const source = status?.Source ?? ''
      if (source) remember(key, source)
      return source
    })
    .catch(() => '')
    .finally(() => inFlight.delete(key))

  inFlight.set(key, request)

  return request
}

// useStatsSource names the query-statistics extension of the connection the
// current route points at. Empty until the answer is in: callers must not read
// an unknown source as a foreign one.
export function useStatsSource() {
  const { clusterName, hostName, databaseName } = useClusterInfo()
  const source = ref('')
  let awaited = ''

  watch([clusterName, hostName, databaseName], async ([cluster, instance, database]) => {
    if (!cluster || !instance || !database) {
      awaited = ''
      source.value = ''
      return
    }

    const key = sourceKey(cluster, instance, database)
    awaited = key

    const resolved = await fetchSource(cluster, instance, database)
    if (awaited === key) source.value = resolved
  }, { immediate: true })

  return { statsSource: computed(() => source.value) }
}
