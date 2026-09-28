<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useViewError } from '@/composables/useViewError'
import { getMaintenanceColdStatus, getMaintenanceInfo } from '@/api/gen/default/default'
import type { ColdTablesStatus, GetMaintenanceInfoActivity, MaintenanceInfo } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader, usePaginatedApiLoader } from '@/composables/useApiLoader'
import { useDebouncedRef } from '@/composables/useDebouncedRef'
import PaginationControls from '@/components/PaginationControls.vue'
import { fmtDateTime } from '@/utils/format'
import { usePrefsStore } from '@/stores/prefs'

const prefs = usePrefsStore()

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
const activity = ref<GetMaintenanceInfoActivity>('all')

const { items: coldStatus } = useApiLoader<ColdTablesStatus | null>(
  () =>
    getMaintenanceColdStatus({
      cluster_name: clusterName.value!,
      instance: hostName.value!,
      database: databaseName.value!,
    }),
  {
    deps: [clusterName, hostName, databaseName],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError,
    defaultValue: null,
  },
)

const coldAvailable = computed(() => coldStatus.value?.status === 'available')

const coldUnavailableHint = computed(() => {
  const s = coldStatus.value
  if (!s || s.status === 'available' || s.status === 'standby' || s.status === 'no_storage') return ''
  return `${t('healthScore.cold.unavailable')}. ${t(`healthScore.cold.reason.${s.status}`, {
    observed: s.observed_days ?? 0,
    days: s.window_days,
  })}`
})

watch(coldAvailable, (ok) => {
  if (!ok) activity.value = 'all'
})

const { items, loading, page, hasMore, load } = usePaginatedApiLoader<MaintenanceInfo>(
  (limit, offset) => getMaintenanceInfo({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
    limit,
    offset,
    table_name: debouncedTableName.value || undefined,
    activity: activity.value,
  }),
  {
    pageSize: () => prefs.pageSize,
    deps: [clusterName, hostName, databaseName, debouncedTableName, activity],
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
      <v-btn-toggle
        v-if="coldAvailable"
        v-model="activity"
        mandatory
        density="compact"
        variant="outlined"
        divided
        class="mr-2"
      >
        <v-btn value="all" size="small">{{ t('maintenance.activity.all') }}</v-btn>
        <v-btn value="active" size="small">{{ t('maintenance.activity.active') }}</v-btn>
        <v-btn value="cold" size="small" prepend-icon="mdi-snowflake">{{ t('maintenance.activity.cold') }}</v-btn>
      </v-btn-toggle>
      <v-tooltip v-else-if="coldUnavailableHint" :text="coldUnavailableHint" location="bottom" max-width="400">
        <template #activator="{ props: tp }">
          <v-icon v-bind="tp" size="small" color="medium-emphasis" class="mr-2">mdi-snowflake-off</v-icon>
        </template>
      </v-tooltip>
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
        <template #item.Table="{ item }">
          {{ item.Table }}
          <v-chip
            v-if="item.NoWritesDays != null"
            size="x-small"
            variant="tonal"
            prepend-icon="mdi-snowflake"
            class="ml-1"
          >
            {{ t('healthScore.cold.noWritesDays', { n: item.NoWritesDays }) }}
          </v-chip>
        </template>
        <template #item.LastVacuum="{ item, value }">
          <span :class="{ 'text-medium-emphasis': item.NoWritesDays != null }">{{ fmtDateTime(value) }}</span>
        </template>
        <template #item.LastAutovacuum="{ item, value }">
          <span :class="{ 'text-medium-emphasis': item.NoWritesDays != null }">{{ fmtDateTime(value) }}</span>
        </template>
        <template #item.LastAnalyze="{ item, value }">
          <span :class="{ 'text-medium-emphasis': item.NoWritesDays != null }">{{ fmtDateTime(value) }}</span>
        </template>
        <template #item.LastAutoanalyze="{ item, value }">
          <span :class="{ 'text-medium-emphasis': item.NoWritesDays != null }">{{ fmtDateTime(value) }}</span>
        </template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
