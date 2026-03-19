<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getCommonSummary,
  getInstanceInfo,
  getStatsResetTime,
  getPgssStatsResetTime,
  getQueryStatsStatus,
  getDatabaseSize,
} from '@/api/gen/default/default'
import type {
  CommonSummary,
  DatabaseSize,
  StatsResetTime,
  QueryStatsStatus,
  InstanceInfo,
} from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

// --- Common Summary ---
const summaryHeaders = computed(() => [
  { title: t('header.namespace'), key: 'Namespace' },
  { title: t('header.kind'), key: 'localizedKind' },
  { title: t('header.approxSize'), key: 'ApproxSize', sortable: false },
  { title: t('header.amount'), key: 'Amount' },
])
const summaryItems = ref<CommonSummary[]>([])
const summaryLoading = ref(false)

const displaySummaryItems = computed(() =>
  summaryItems.value.map(item => ({
    ...item,
    localizedKind: t(item.Kind),
  }))
)

async function loadSummary() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  summaryLoading.value = true
  try {
    const response = await getCommonSummary({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    summaryItems.value = assertOk(response) ?? []
  } catch (err) {
    emit('error', String(err))
    summaryItems.value = []
  } finally {
    summaryLoading.value = false
  }
}

// --- In Recovery + Version Info ---
const instanceInfo = ref<InstanceInfo | null>(null)

const hostRole = computed(() => {
  if (instanceInfo.value === null) return null
  return instanceInfo.value.InRecovery ? 'REPLICA' : 'MASTER'
})

const hostRoleColor = computed(() => {
  if (instanceInfo.value === null) return 'default'
  return instanceInfo.value.InRecovery ? 'info' : 'success'
})

async function loadInstanceInfo() {
  if (!clusterName.value || !hostName.value) return
  try {
    const response = await getInstanceInfo({
      cluster_name: clusterName.value,
      instance: hostName.value,
    })
    instanceInfo.value = assertOk<InstanceInfo>(response)
  } catch (err) {
    emit('error', String(err))
    instanceInfo.value = null
  }
}

// --- Database Size ---
const databaseSize = ref<DatabaseSize | null>(null)

async function loadDatabaseSize() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  try {
    const response = await getDatabaseSize({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    databaseSize.value = assertOk<DatabaseSize>(response)
  } catch {
    databaseSize.value = null
  }
}

// --- pgss availability ---
const pgssAvailable = ref(false)
const pgssInfoSupported = computed(() => (instanceInfo.value?.VersionNum ?? 0) >= 140000)

async function loadQueryStatsStatus() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  try {
    const response = await getQueryStatsStatus({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    const s = assertOk<QueryStatsStatus>(response)
    pgssAvailable.value = !!(s?.Available && s?.Enabled && s?.Readable)
  } catch {
    pgssAvailable.value = false
  }
}

// --- Stats Reset Time ---
const statsResetTime = ref<string | null>(null)
const pgssStatsResetTime = ref<string | null>(null)
const statsResetTimeLoading = ref(false)

async function loadStatsResetTime() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  statsResetTimeLoading.value = true
  try {
    const statsRes = await getStatsResetTime({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    const data = assertOk<StatsResetTime[]>(statsRes)
    statsResetTime.value = data?.length ? data[0].Time : null
  } catch {
    statsResetTime.value = null
  }

  if (pgssAvailable.value && pgssInfoSupported.value) {
    try {
      const pgssRes = await getPgssStatsResetTime({
        cluster_name: clusterName.value,
        instance: hostName.value,
        database: databaseName.value,
      })
      pgssStatsResetTime.value = assertOk<StatsResetTime>(pgssRes)?.Time ?? null
    } catch {
      pgssStatsResetTime.value = null
    }
  } else {
    pgssStatsResetTime.value = null
  }

  statsResetTimeLoading.value = false
}

function formatDateTime(iso: string): string {
  try {
    const d = new Date(iso)
    if (isNaN(d.getTime()) || d.getFullYear() < 2000) return ''
    return d.toLocaleString()
  } catch {
    return iso
  }
}

// --- Load all ---
async function load() {
  try {
    await Promise.allSettled([loadInstanceInfo(), loadQueryStatsStatus()])
    await Promise.allSettled([
      loadSummary(),
      loadDatabaseSize(),
      loadStatsResetTime(),
    ])
  } catch (err) {
    emit('error', String(err))
  }
}

watch([clusterName, hostName, databaseName], () => load(), { immediate: true })
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      {{ t('Common summary') }}
      <v-tooltip :text="t('hint.approxSize')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <div class="d-flex flex-wrap ga-2 mb-3">
        <v-chip v-if="hostRole" :color="hostRoleColor" variant="tonal">
          {{ hostRole }}
        </v-chip>
        <v-chip v-if="databaseSize" variant="tonal">
          {{ t('home.databaseSize') }}: {{ databaseSize.SizePretty }}
        </v-chip>
        <v-chip v-if="statsResetTime && formatDateTime(statsResetTime)" variant="tonal">
          {{ t('home.statsResetAt') }}: {{ formatDateTime(statsResetTime) }}
        </v-chip>
        <v-chip v-if="!statsResetTimeLoading && statsResetTime && !formatDateTime(statsResetTime)" variant="tonal">
          {{ t('home.statsNeverReset') }}
        </v-chip>
        <v-chip v-if="pgssAvailable && pgssInfoSupported && pgssStatsResetTime && formatDateTime(pgssStatsResetTime)" variant="tonal">
          {{ t('home.pgssStatsResetAt') }}: {{ formatDateTime(pgssStatsResetTime) }}
        </v-chip>
        <v-chip v-if="pgssAvailable && pgssInfoSupported && !statsResetTimeLoading && !pgssStatsResetTime" variant="tonal">
          {{ t('home.pgssStatsNeverReset') }}
        </v-chip>
        <v-tooltip v-if="instanceInfo?.VersionFull" :text="instanceInfo.VersionFull" location="bottom">
          <template #activator="{ props }">
            <v-chip v-bind="props" variant="tonal">
              PostgreSQL {{ instanceInfo.Version }}
            </v-chip>
          </template>
        </v-tooltip>
      </div>

      <v-data-table
        :headers="summaryHeaders"
        :items="displaySummaryItems"
        :loading="summaryLoading"
        density="compact"
        multi-sort
        :items-per-page="-1"
        hide-default-footer
      />
    </v-card-text>
  </v-card>
</template>
