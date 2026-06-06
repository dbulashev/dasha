<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { getHealthScoreDatabases } from '@/api/gen/default/default'
import type { HealthScoreDatabase, HealthScoreDatabases } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()
const router = useRouter()

const { items: data, loading } = useApiLoader<HealthScoreDatabases | null>(
  () =>
    getHealthScoreDatabases({
      cluster_name: clusterName.value!,
      instance: hostName.value!,
    }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError,
    defaultValue: null,
  },
)

const databases = computed<HealthScoreDatabase[]>(() => data.value?.databases ?? [])
const worstDatabase = computed<string | null>(() => data.value?.worst_database ?? null)

const worstScore = computed<number | null>(() => {
  const w = worstDatabase.value
  if (!w) return null
  return databases.value.find((d) => d.database === w)?.score ?? null
})

const headers = computed(() => [
  { title: t('healthScore.databases.headers.database'), key: 'database' },
  { title: t('healthScore.databases.headers.size'), key: 'size_bytes' },
  { title: t('healthScore.databases.headers.score'), key: 'score' },
])

// The red band (< 40) is what the backend's critical floor targets
// (health.criticalScoreCeiling = 30); keep these thresholds in sync with it.
function scoreColor(score: number): string {
  if (score >= 95) return 'success'
  if (score >= 70) return 'warning'
  if (score >= 40) return 'orange'
  return 'error'
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 ** 2) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 ** 3) return (bytes / 1024 ** 2).toFixed(1) + ' MB'
  return (bytes / 1024 ** 3).toFixed(1) + ' GB'
}

function selectDatabase(name: string) {
  router.push({
    name: 'HealthScore',
    params: { clustername: router.currentRoute.value.params.clustername },
    query: { ...router.currentRoute.value.query, database: name },
  })
}
</script>

<template>
  <v-card>
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-database" />
      {{ t('healthScore.databases.title') }}
    </v-card-title>
    <v-card-text>
      <v-alert
        v-if="worstDatabase && worstScore !== null && worstScore < 70"
        :type="worstScore < 40 ? 'error' : 'warning'"
        variant="tonal"
        density="compact"
        class="mb-3"
      >
        {{ t('healthScore.databases.worstHint', { db: worstDatabase, score: Math.round(worstScore) }) }}
      </v-alert>

      <v-skeleton-loader v-if="loading" type="table-row@3" />
      <v-data-table
        v-else
        :headers="headers"
        :items="databases"
        :sort-by="[{ key: 'score', order: 'asc' }]"
        @click:row="(_: unknown, { item }: { item: HealthScoreDatabase }) => selectDatabase(item.database)"
      >
        <template #item.size_bytes="{ item }">
          {{ formatBytes((item as HealthScoreDatabase).size_bytes) }}
        </template>
        <template #item.score="{ item }">
          <v-chip
            :color="scoreColor((item as HealthScoreDatabase).score)"
            variant="flat"
            size="small"
          >
            {{ Math.round((item as HealthScoreDatabase).score) }}
          </v-chip>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
