<script setup lang="ts">
import { computed, ref } from 'vue'
import { Bar } from 'vue-chartjs'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  BarElement,
  Tooltip,
} from 'chart.js'
import { useI18n } from 'vue-i18n'
import { getQueriesTop10Chart } from '@/api/gen/default/default'
import type { QueryTop10Chart, QueryTop10ChartItem } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import { copyToClipboard } from '@/utils/sql'

ChartJS.register(CategoryScale, LinearScale, BarElement, Tooltip)

const CHART_COLORS = [
  '#4CAF50', '#2196F3', '#FF9800', '#9C27B0', '#F44336',
  '#00BCD4', '#795548', '#607D8B', '#E91E63', '#CDDC39',
]

const METRICS: (keyof QueryTop10Chart)[] = [
  'Calls', 'TotalExecTime', 'Rows',
  'SharedBlksHit', 'SharedBlksRead', 'SharedBlksDirtied',
  'TempBlksRead', 'TempBlksWritten', 'WalRecords',
]

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()

const { items: chartData, loading } = useApiLoader<QueryTop10Chart | null>(
  () => getQueriesTop10Chart({
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

const barData = computed(() => {
  if (!chartData.value) return null

  const allQueryIds = new Set<string>()
  for (const metric of METRICS) {
    const items = chartData.value[metric] as QueryTop10ChartItem[]
    if (items) {
      for (const item of items) {
        allQueryIds.add(item.QueryID)
      }
    }
  }

  const queryIdList = [...allQueryIds]

  const datasets = queryIdList.map((qid, idx) => ({
    label: String(qid),
    data: METRICS.map(metric => {
      const items = chartData.value![metric] as QueryTop10ChartItem[]
      return items?.find(i => i.QueryID === qid)?.Pct ?? 0
    }),
    backgroundColor: CHART_COLORS[idx % CHART_COLORS.length],
  }))

  return {
    labels: METRICS.map(m => t(`chart.metric.${m}`)),
    datasets,
  }
})

const snackbar = ref(false)
const copiedQueryId = ref('')

function onChartClick(_event: unknown, elements: { datasetIndex: number }[]) {
  if (!elements.length || !barData.value || elements[0] == undefined) return
  const idx = elements[0].datasetIndex
  if (barData.value.datasets[idx] == undefined) return
  const queryId = barData.value.datasets[idx].label
  copyToClipboard(queryId)
  copiedQueryId.value = queryId
  snackbar.value = true
}

function onChartHover(event: { native: MouseEvent | null }, elements: unknown[]) {
  const canvas = event.native?.target as HTMLCanvasElement | null
  if (canvas) {
    canvas.style.cursor = elements.length ? 'pointer' : 'default'
  }
}

const barOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  onClick: onChartClick,
  onHover: onChartHover,
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (ctx: { dataset: { label: string }; parsed: { y: number } }) =>
          `Query ${ctx.dataset.label}: ${ctx.parsed.y.toFixed(2)}%`,
        afterBody: () => t('chart.clickToCopy'),
      },
    },
  },
  scales: {
    x: { stacked: true },
    y: { stacked: true, min: 0, max: 100, ticks: { callback: (v: number) => `${v}%` } },
  },
}))
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-chart-bar-stacked" />{{ t('Top 10 Queries Chart') }}
    </v-card-title>
    <v-card-text>
      <v-skeleton-loader v-if="loading" type="image" height="320" />
      <div v-else-if="barData && barData.datasets.length" class="chart-container">
        <Bar :data="barData" :options="barOptions as any" />
      </div>
      <v-alert v-else type="info" variant="tonal">{{ t('noData') }}</v-alert>
    </v-card-text>
  </v-card>

  <v-snackbar v-model="snackbar" :timeout="2000" color="success" location="bottom">
    {{ t('chart.copied', { queryId: copiedQueryId }) }}
  </v-snackbar>
</template>

<style scoped>
.chart-container {
  width: 100%;
  height: 320px;
}
</style>
