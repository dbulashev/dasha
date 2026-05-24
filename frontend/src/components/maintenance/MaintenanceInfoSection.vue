<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useViewError } from '@/composables/useViewError'
import { getMaintenanceInfo } from '@/api/gen/default/default'
import type { MaintenanceInfo } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { usePaginatedApiLoader } from '@/composables/useApiLoader'
import { useDebouncedRef } from '@/composables/useDebouncedRef'
import { DEFAULT_PAGE_SIZE } from '@/constants/pagination'
import PaginationControls from '@/components/PaginationControls.vue'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()

const headers = computed(() => [
  { title: t('header.schema'), key: 'Schema' },
  { title: t('header.table'), key: 'Table' },
  { title: t('maintenance.lastVacuum'), key: 'LastVacuum' },
  { title: t('maintenance.lastAutovacuum'), key: 'LastAutovacuum' },
  { title: t('maintenance.lastAnalyze'), key: 'LastAnalyze' },
  { title: t('maintenance.lastAutoanalyze'), key: 'LastAutoanalyze' },
  { title: t('maintenance.deadRows'), key: 'DeadRows' },
  { title: t('maintenance.liveRows'), key: 'LiveRows' },
])

const tableName = ref('')
const debouncedTableName = useDebouncedRef(tableName, 500)

function formatDateTime(iso: string | null): string {
  if (!iso) return '—'
  try {
    const d = new Date(iso)
    if (isNaN(d.getTime()) || d.getFullYear() < 2000) return '—'
    return d.toLocaleString()
  } catch {
    return iso
  }
}

const { items, loading, page, hasMore, load } = usePaginatedApiLoader<MaintenanceInfo>(
  (limit, offset) => getMaintenanceInfo({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
    limit,
    offset,
    table_name: debouncedTableName.value || undefined,
  }),
  {
    pageSize: DEFAULT_PAGE_SIZE,
    deps: [clusterName, hostName, databaseName, debouncedTableName],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError,
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      {{ t('maintenance.info') }}
      <v-tooltip :text="t('hint.maintenanceInfo')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
      <v-spacer />
      <v-text-field
        v-model="tableName"
        :label="t('header.table')"
        density="compact"
        hide-details
        clearable
        style="max-width: 300px"
      />
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading">
        <template #item.LastVacuum="{ value }">{{ formatDateTime(value) }}</template>
        <template #item.LastAutovacuum="{ value }">{{ formatDateTime(value) }}</template>
        <template #item.LastAnalyze="{ value }">{{ formatDateTime(value) }}</template>
        <template #item.LastAutoanalyze="{ value }">{{ formatDateTime(value) }}</template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
