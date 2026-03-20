<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getDatabaseUsers, getQueriesReport } from '@/api/gen/default/default'
import type { QueryReport } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useExcludeUsersStore } from '@/stores/excludeUsers'
import ReportCard from '@/components/queries/ReportCard.vue'
import SqlDialog from '@/components/queries/SqlDialog.vue'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()
const excludeUsersStore = useExcludeUsersStore()

type ReportSortKey = 'total_time' | 'calls' | 'wal' | 'rows' | 'cpu_time' | 'io_time' | 'temp_blks'

const reportSortBy = ref<ReportSortKey>('total_time')

const reportSortOptions = computed(() => [
  { value: 'total_time', title: t('report.sort.total_time') },
  { value: 'calls', title: t('report.sort.calls') },
  { value: 'wal', title: t('report.sort.wal') },
  { value: 'rows', title: t('report.sort.rows') },
  { value: 'cpu_time', title: t('report.sort.cpu_time') },
  { value: 'io_time', title: t('report.sort.io_time') },
  { value: 'temp_blks', title: t('report.sort.temp_blks') },
])

const sortFieldMap: Record<ReportSortKey, keyof QueryReport> = {
  total_time: 'TotalTimeMs',
  calls: 'Calls',
  wal: 'WalBytes',
  rows: 'Rows',
  cpu_time: 'CpuTimeMs',
  io_time: 'IoTimeMs',
  temp_blks: 'TempBlks',
}

// Exclude users filter — restore from store
const excludeUsers = ref<string[]>(
  clusterName.value
    ? excludeUsersStore.getExcludeUsers(clusterName.value)
    : [],
)

watch(clusterName, () => {
  if (clusterName.value) {
    excludeUsers.value = excludeUsersStore.getExcludeUsers(clusterName.value)
  }
})

watch(excludeUsers, (val) => {
  if (clusterName.value) {
    excludeUsersStore.setExcludeUsers(clusterName.value, val)
  }
})

const { items: availableUsers } = useApiLoader<string[]>(
  () => getDatabaseUsers({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: () => {},
  },
)

const { items, loading } = useApiLoader<QueryReport[]>(
  () => getQueriesReport({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    exclude_users: excludeUsers.value.length ? excludeUsers.value : undefined,
  }),
  {
    deps: [clusterName, hostName, excludeUsers],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: (msg) => emit('error', msg),
  },
)

const sortedItems = computed(() => {
  const field = sortFieldMap[reportSortBy.value]
  return [...items.value].sort((a, b) => {
    const va = (a[field] as number | null | undefined) ?? 0
    const vb = (b[field] as number | null | undefined) ?? 0
    return vb - va
  })
})

const hasNegativeCpuTime = computed(() =>
  items.value.some((row) => row.CpuTimeMs != null && row.CpuTimeMs < 0),
)

// SQL dialog
const sqlDialogVisible = ref(false)
const sqlDialogText = ref('')
const sqlDialogQueryId = ref<number>(0)

function showSqlDialog(item: QueryReport) {
  sqlDialogQueryId.value = item.QueryID
  sqlDialogText.value = item.Query
  sqlDialogVisible.value = true
}
</script>

<template>
  <v-alert v-if="hasNegativeCpuTime" type="warning" class="mb-4" closable>
    {{ t('cpuTimeWarning') }}
  </v-alert>

  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      <span>{{ t('Query Report') }}</span>
      <v-tooltip :text="t('hint.queryStats')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
      <v-select
        v-model="reportSortBy"
        :items="reportSortOptions"
        :label="t('report.sortBy')"
        density="compact"
        variant="outlined"
        hide-details
        class="ml-4"
        style="max-width: 220px;"
      />
      <v-combobox
        v-model="excludeUsers"
        :items="availableUsers"
        :label="t('report.excludeUsers')"
        density="compact"
        variant="outlined"
        hide-details
        multiple
        chips
        closable-chips
        class="ml-4"
        style="max-width: 350px;"
      />
    </v-card-title>
    <v-card-text>
      <v-progress-linear v-if="loading" indeterminate />
      <div v-else-if="sortedItems.length">
        <ReportCard
          v-for="item in sortedItems"
          :key="item.QueryID"
          :item="item"
          :sort-by="reportSortBy"
          @show-sql="showSqlDialog"
        />
      </div>
      <div v-else-if="!loading" class="text-medium-emphasis">{{ t('noData') }}</div>
    </v-card-text>
  </v-card>

  <SqlDialog v-model="sqlDialogVisible" :query-id="sqlDialogQueryId" :sql="sqlDialogText" />
</template>
