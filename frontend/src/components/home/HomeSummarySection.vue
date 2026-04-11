<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { Doughnut } from 'vue-chartjs'
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js'
import { useI18n } from 'vue-i18n'

ChartJS.register(ArcElement, Tooltip, Legend)
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
import { useViewError } from '@/composables/useViewError'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()

// --- Common Summary ---
const summaryHeaders = computed(() => [
  { title: t('header.kind'), key: 'kindWithNamespace' },
  { title: t('header.approxSize'), key: 'ApproxSize', sortable: false },
  { title: t('header.amount'), key: 'Amount' },
])
const summaryItems = ref<CommonSummary[]>([])
const summaryLoading = ref(false)

const displaySummaryItems = computed(() =>
  summaryItems.value.map(item => ({
    ...item,
    kindWithNamespace: `${t(item.Kind)} (${item.Namespace})`,
  }))
)

// --- Donut chart ---
const CHART_COLORS = [
  '#4CAF50', '#2196F3', '#FF9800', '#9C27B0', '#F44336',
  '#00BCD4', '#795548', '#607D8B', '#E91E63', '#CDDC39',
]

const chartItems = computed(() =>
  summaryItems.value.filter(i => i.ApproxSizeBytes > 0)
)

const chartData = computed(() => {
  const items = chartItems.value
  return {
    labels: items.map(i => `${i.Namespace}.${t(i.Kind)}`),
    datasets: [{
      data: items.map(i => i.ApproxSizeBytes),
      backgroundColor: items.map((_, idx) => CHART_COLORS[idx % CHART_COLORS.length]),
      hoverOffset: 4,
    }],
  }
})

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'kB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i]
}

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: {
      display: false,
    },
    tooltip: {
      callbacks: {
        title: () => '',
        label: (ctx: { label: string; parsed: number; dataset: { data: number[] } }) => {
          const total = ctx.dataset.data.reduce((a: number, b: number) => a + b, 0)
          const pct = ((ctx.parsed / total) * 100).toFixed(1)
          return `${ctx.label}: ${formatBytes(ctx.parsed)} (${pct}%)`
        },
      },
    },
  },
}))

const hasChartData = computed(() => summaryItems.value.some(i => i.ApproxSizeBytes > 0))

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
    onError(getErrorMessage(err), err)
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
    onError(getErrorMessage(err), err)
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
    statsResetTime.value = data && data.length > 0 ? data[0]!.Time : null
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
    onError(getErrorMessage(err), err)
  }
}

watch([clusterName, hostName, databaseName], () => load(), { immediate: true })
</script>

<template>
  <!-- Status banner -->
  <v-card variant="outlined" class="mb-4">
    <v-card-text class="d-flex flex-wrap align-center ga-3">
      <v-chip v-if="hostRole" :color="hostRoleColor" variant="flat" size="default">
        <v-icon start :icon="hostRole === 'MASTER' ? 'mdi-crown' : 'mdi-content-copy'" />
        {{ hostRole }}
      </v-chip>
      <v-tooltip v-if="instanceInfo?.VersionFull" :text="instanceInfo.VersionFull" location="bottom">
        <template #activator="{ props }">
          <v-chip v-bind="props" variant="flat" color="indigo" size="default">
            <v-icon start icon="mdi-elephant" />
            PostgreSQL {{ instanceInfo.Version }}
          </v-chip>
        </template>
      </v-tooltip>
      <v-chip v-if="databaseSize" variant="flat" color="teal" size="default">
        <v-icon start icon="mdi-database" />
        {{ databaseSize.SizePretty }}
      </v-chip>
      <v-chip v-if="statsResetTime && formatDateTime(statsResetTime)" variant="tonal" size="default" prepend-icon="mdi-clock-outline">
        {{ t('home.statsResetAt') }}: {{ formatDateTime(statsResetTime) }}
      </v-chip>
      <v-chip v-if="!statsResetTimeLoading && statsResetTime && !formatDateTime(statsResetTime)" variant="tonal" size="default" prepend-icon="mdi-clock-outline">
        {{ t('home.statsNeverReset') }}
      </v-chip>
      <v-chip v-if="pgssAvailable && pgssInfoSupported && pgssStatsResetTime && formatDateTime(pgssStatsResetTime)" variant="tonal" size="default" prepend-icon="mdi-clock-outline">
        {{ t('home.pgssStatsResetAt') }}: {{ formatDateTime(pgssStatsResetTime) }}
      </v-chip>
      <v-chip v-if="pgssAvailable && pgssInfoSupported && !statsResetTimeLoading && !pgssStatsResetTime" variant="tonal" size="default" prepend-icon="mdi-clock-outline">
        {{ t('home.pgssStatsNeverReset') }}
      </v-chip>
    </v-card-text>
  </v-card>

  <!-- Common summary table + chart -->
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-sigma" />
      {{ t('Common summary') }}
      <v-tooltip :text="t('hint.approxSize')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-row>
        <v-col cols="12" :md="hasChartData ? 8 : 12">
          <v-data-table
            :headers="summaryHeaders"
            :items="displaySummaryItems"
            :loading="summaryLoading"
          />
        </v-col>
        <v-col v-if="hasChartData && !summaryLoading" cols="12" md="4" class="d-flex align-center justify-center">
          <div class="chart-container">
            <Doughnut :data="chartData" :options="chartOptions" />
          </div>
        </v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.chart-container {
  width: 100%;
  max-width: 360px;
  height: 240px;
}
</style>
