<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getQueriesCompare,
  getDatabaseUsers,
  getSnapshots,
  getSnapshotsStatus,
  getPgssStatsResetTime,
} from '@/api/gen/default/default'
import type {
  QueryCompareItem,
  SnapshotListItem,
  StatsResetTime,
} from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import { useExcludeUsersStore } from '@/stores/excludeUsers'
import { assertOk } from '@/utils/api'
import { fmtAge } from '@/utils/format'
import type { CompareSortKey } from '@/components/queries/compare-types'
import { compareSortFieldMap } from '@/components/queries/compare-types'
import CompareCard from '@/components/queries/CompareCard.vue'
import SqlDialog from '@/components/queries/SqlDialog.vue'

const { clusterName, hostName, databaseName } = useClusterInfo()
const { t } = useI18n()
const { clearError, onError } = useViewError()
const excludeUsersStore = useExcludeUsersStore()

const excludeUsers = ref<string[]>(
  clusterName.value ? excludeUsersStore.getExcludeUsers(clusterName.value) : [],
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

const isLiveB = computed(() => selectedB.value === null)

const snapshotsAvailable = ref(false)
const snapshotsList = ref<SnapshotListItem[]>([])

const selectedA = ref<string | null>(null)
const selectedB = ref<string | null>(null) // null = live

const sortBy = ref<CompareSortKey>('total_time')
const hideAbsentInA = ref(false)
const hideAbsentInB = ref(false)

const sortOptions = computed(() => [
  { value: 'total_time', title: t('report.sort.total_time') },
  { value: 'calls', title: t('report.sort.calls') },
  { value: 'wal', title: t('report.sort.wal') },
  { value: 'rows', title: t('report.sort.rows') },
  { value: 'cpu_time', title: t('report.sort.cpu_time') },
  { value: 'io_time', title: t('report.sort.io_time') },
  { value: 'temp_blks', title: t('report.sort.temp_blks') },
])

const selectorAItems = computed(() =>
  snapshotsList.value.map(s => ({
    value: s.Id,
    title: new Date(s.CreatedAt).toLocaleString(),
  })),
)

const selectorBItems = computed(() => {
  const live = { value: null as string | null, title: t('compare.liveData') }
  const items = snapshotsList.value.map(s => ({
    value: s.Id as string | null,
    title: new Date(s.CreatedAt).toLocaleString(),
  }))
  return [live, ...items]
})

const compareData = ref<QueryCompareItem[] | null>(null)
const loading = ref(false)

const livePgssStatsReset = ref<string | null>(null)

async function loadLivePgssReset() {
  if (!clusterName.value || !hostName.value || !databaseName.value) {
    livePgssStatsReset.value = null
    return
  }
  try {
    const res = await getPgssStatsResetTime({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    const body = assertOk<StatsResetTime>(res)
    livePgssStatsReset.value = body?.Time ?? null
  } catch {
    livePgssStatsReset.value = null
  }
}

const ageUnknown = computed(() => t('compare.ageUnknown'))

const ageA = computed(() => {
  if (!selectedA.value) return ''
  const snap = snapshotsList.value.find(s => s.Id === selectedA.value)
  if (!snap) return ''
  return fmtAge(snap.CreatedAt, snap.PgssStatsReset ?? undefined, ageUnknown.value)
})

const ageB = computed(() => {
  if (selectedB.value) {
    const snap = snapshotsList.value.find(s => s.Id === selectedB.value)
    if (!snap) return ''
    return fmtAge(snap.CreatedAt, snap.PgssStatsReset ?? undefined, ageUnknown.value)
  }
  if (!livePgssStatsReset.value) return ''
  return fmtAge(new Date().toISOString(), livePgssStatsReset.value, ageUnknown.value)
})

const filteredItems = computed(() => {
  if (!compareData.value) return []
  let items = compareData.value
  if (hideAbsentInA.value) {
    items = items.filter(i => i.Left != null)
  }
  if (hideAbsentInB.value) {
    items = items.filter(i => i.Right != null)
  }
  return items
})

const sortedItems = computed(() => {
  const field = compareSortFieldMap[sortBy.value].value
  return [...filteredItems.value].sort((a, b) => {
    const va = (a.Left?.[field] as number | null | undefined) ?? (a.Right?.[field] as number | null | undefined) ?? 0
    const vb = (b.Left?.[field] as number | null | undefined) ?? (b.Right?.[field] as number | null | undefined) ?? 0
    return (vb as number) - (va as number)
  })
})

const totalCount = computed(() => compareData.value?.length ?? 0)
const onlyInA = computed(() => compareData.value?.filter(i => i.Right == null).length ?? 0)
const onlyInB = computed(() => compareData.value?.filter(i => i.Left == null).length ?? 0)
const inBoth = computed(() => totalCount.value - onlyInA.value - onlyInB.value)

const sqlDialogVisible = ref(false)
const sqlDialogText = ref('')
const sqlDialogQueryId = ref<number>(0)

function showSqlDialog(item: QueryCompareItem) {
  sqlDialogQueryId.value = item.QueryID
  sqlDialogText.value = item.Query
  sqlDialogVisible.value = true
}

async function loadSnapshotsStatus() {
  try {
    const res = await getSnapshotsStatus()
    const body = assertOk<{ Available: boolean }>(res)
    snapshotsAvailable.value = body.Available
  } catch {
    snapshotsAvailable.value = false
  }
}

async function loadSnapshotsList() {
  if (!snapshotsAvailable.value || !clusterName.value || !hostName.value || !databaseName.value) {
    snapshotsList.value = []
    return
  }
  try {
    const res = await getSnapshots({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    snapshotsList.value = assertOk<SnapshotListItem[]>(res) ?? []
  } catch {
    snapshotsList.value = []
  }
}

async function loadCompare() {
  if (!selectedA.value || !clusterName.value || !hostName.value || !databaseName.value) {
    compareData.value = null
    return
  }
  loading.value = true
  clearError()
  try {
    const params: Record<string, unknown> = {
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      snapshot_a: selectedA.value,
    }
    if (selectedB.value) {
      params.snapshot_b = selectedB.value
    }
    if (!selectedB.value && excludeUsers.value.length) {
      params.exclude_users = excludeUsers.value
    }
    const res = await getQueriesCompare(params as Parameters<typeof getQueriesCompare>[0])
    compareData.value = assertOk<QueryCompareItem[]>(res)
  } catch (err) {
    onError(String(err), err)
    compareData.value = null
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName, databaseName], async () => {
  clearError()
  compareData.value = null
  selectedA.value = null
  selectedB.value = null
  loadLivePgssReset()
  await loadSnapshotsStatus()
  await loadSnapshotsList()
}, { immediate: true })

watch([selectedA, selectedB, excludeUsers], () => {
  loadCompare()
})
</script>

<template>
  <v-alert v-if="!snapshotsAvailable" type="info" class="mb-4">
    {{ t('compare.storageUnavailable') }}
  </v-alert>

  <div v-else>
    <v-card class="mb-4 compare-sticky-header" elevation="2">
      <v-card-text class="py-2">
        <v-row dense align="center">
          <v-col cols="12" sm="6" class="d-flex align-center ga-2">
            <span class="text-caption font-weight-bold">A:</span>
            <v-select
              v-model="selectedA"
              :items="selectorAItems"
              :label="t('compare.snapshotA')"
              density="compact"
              variant="outlined"
              hide-details
              style="max-width: 260px;"
            />
            <span v-if="ageA && compareData" class="text-caption text-medium-emphasis">{{ t('compare.age') }}: {{ ageA }}</span>
          </v-col>
          <v-col cols="12" sm="6" class="d-flex align-center ga-2">
            <span class="text-caption font-weight-bold">B:</span>
            <v-select
              v-model="selectedB"
              :items="selectorBItems"
              :label="t('compare.snapshotB')"
              density="compact"
              variant="outlined"
              hide-details
              style="max-width: 260px;"
            />
            <span v-if="ageB && compareData" class="text-caption text-medium-emphasis">{{ t('compare.age') }}: {{ ageB }}</span>
          </v-col>
        </v-row>
        <v-row dense align="center" class="mt-1">
          <v-col cols="auto">
            <v-checkbox v-model="hideAbsentInA" :label="t('compare.hideAbsentA')" density="compact" hide-details />
          </v-col>
          <v-col cols="auto">
            <v-checkbox v-model="hideAbsentInB" :label="t('compare.hideAbsentB')" density="compact" hide-details />
          </v-col>
          <v-col cols="auto">
            <v-select
              v-model="sortBy"
              :items="sortOptions"
              :label="t('report.sortBy')"
              density="compact"
              variant="outlined"
              hide-details
              style="max-width: 200px;"
            />
          </v-col>
          <v-col v-if="isLiveB" cols="auto">
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
              style="width: 350px;"
            />
          </v-col>
          <v-spacer />
          <v-col v-if="compareData" cols="auto" class="text-caption text-medium-emphasis">
            {{ t('compare.stats', { total: totalCount, both: inBoth, onlyA: onlyInA, onlyB: onlyInB }) }}
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <v-progress-linear v-if="loading" indeterminate class="mb-4" />
    <template v-else-if="compareData && sortedItems.length">
      <CompareCard
        v-for="item in sortedItems"
        :key="item.QueryID"
        :item="item"
        :sort-by="sortBy"
        @show-sql="showSqlDialog"
      />
    </template>
    <div v-else-if="compareData && !sortedItems.length" class="text-medium-emphasis">
      {{ t('noData') }}
    </div>
    <div v-else-if="!selectedA" class="text-medium-emphasis">
      {{ t('compare.selectSnapshots') }}
    </div>
  </div>

  <SqlDialog v-model="sqlDialogVisible" :query-id="sqlDialogQueryId" :sql="sqlDialogText" />
</template>

<style scoped>
.compare-sticky-header {
  position: sticky;
  top: 0;
  z-index: 10;
}
</style>
