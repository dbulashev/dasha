<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Line } from 'vue-chartjs'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
} from 'chart.js'
import { useI18n } from 'vue-i18n'
import { getHealthScoreHistory } from '@/api/gen/default/default'
import type { HealthScoreHistory } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import { fmtChartTime } from '@/utils/format'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()

// range -> [span seconds, step seconds]
const RANGES: Record<string, [number, number]> = {
  '24h': [24 * 3600, 300],
  '2d': [2 * 24 * 3600, 600],
  '7d': [7 * 24 * 3600, 1800],
  '14d': [14 * 24 * 3600, 3600],
  '30d': [30 * 24 * 3600, 3600],
}

const selectedRange = ref<'24h' | '2d' | '7d' | '14d' | '30d'>('24h')
const loading = ref(false)
const unavailable = ref(false)
const history = ref<HealthScoreHistory | null>(null)

// Monotonic nonce: rapid watch triggers (e.g. range switches) can race, so each
// call captures an id and only the latest response is allowed to mutate state.
let requestId = 0

async function load() {
  if (!clusterName.value || !hostName.value) return

  const id = ++requestId
  loading.value = true
  unavailable.value = false

  try {
    const [span, step] = RANGES[selectedRange.value]
    const now = Date.now()
    const res = await getHealthScoreHistory({
      cluster_name: clusterName.value,
      instance: hostName.value,
      from: new Date(now - span * 1000).toISOString(),
      to: new Date(now).toISOString(),
      step_seconds: step,
    })
    if (id !== requestId) return
    history.value = assertOk<HealthScoreHistory>(res)
  } catch {
    // 404 / error: metrics datasource not configured or target unmapped — the
    // trend is supplementary, so degrade gracefully instead of erroring the view.
    if (id !== requestId) return
    history.value = null
    unavailable.value = true
  } finally {
    // Only the latest in-flight request clears the spinner.
    if (id === requestId) loading.value = false
  }
}

watch([clusterName, hostName, selectedRange], load, { immediate: true })

function fmtLabel(iso: string): string {
  return fmtChartTime(iso, selectedRange.value !== '24h')
}

const chartData = computed(() => {
  const h = history.value
  if (!h || !h.points.length) return null

  const dipTimes = new Set(h.dips.map((d) => d.time))
  const baseMap = new Map(h.baseline.map((b) => [b.time, b.value]))

  return {
    labels: h.points.map((p) => fmtLabel(p.time)),
    datasets: [
      {
        label: t('healthScore.trend.score'),
        data: h.points.map((p) => p.score),
        borderColor: '#4CAF50',
        backgroundColor: '#4CAF50',
        pointBackgroundColor: h.points.map((p) => (dipTimes.has(p.time) ? '#F44336' : '#4CAF50')),
        pointRadius: h.points.map((p) => (dipTimes.has(p.time) ? 4 : 1.5)),
        tension: 0.25,
        yAxisID: 'y',
      },
      {
        label: t('healthScore.trend.baseline'),
        data: h.points.map((p) => baseMap.get(p.time) ?? null),
        borderColor: '#9E9E9E',
        borderDash: [5, 5],
        pointRadius: 0,
        tension: 0.25,
        yAxisID: 'y',
      },
      {
        label: t('healthScore.trend.latency'),
        data: h.points.map((p) => p.latency_ms),
        borderColor: '#2196F3',
        pointRadius: 0,
        tension: 0.25,
        yAxisID: 'y1',
      },
    ],
  }
})

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: 'index' as const, intersect: false },
  plugins: {
    legend: { display: true, position: 'bottom' as const },
  },
  scales: {
    x: { ticks: { maxTicksLimit: 8, maxRotation: 0 } },
    y: { min: 0, max: 100, position: 'left' as const, title: { display: true, text: 'Score' } },
    y1: {
      position: 'right' as const,
      beginAtZero: true,
      grid: { drawOnChartArea: false },
      title: { display: true, text: t('healthScore.trend.latencyAxis') },
    },
  },
}))
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1 flex-wrap">
      <v-icon start icon="mdi-chart-line" />
      {{ t('healthScore.trend.title') }}
      <v-icon size="small" icon="mdi-help-circle-outline" class="ms-1 text-medium-emphasis">
        <v-tooltip activator="parent" location="bottom" max-width="360">
          {{ t('healthScore.trend.baselineHelp') }}
        </v-tooltip>
      </v-icon>
      <v-spacer />
      <v-btn-toggle v-model="selectedRange" density="compact" variant="outlined" mandatory>
        <v-btn value="24h" size="small">{{ t('healthScore.trend.range.24h') }}</v-btn>
        <v-btn value="2d" size="small">{{ t('healthScore.trend.range.2d') }}</v-btn>
        <v-btn value="7d" size="small">{{ t('healthScore.trend.range.7d') }}</v-btn>
        <v-btn value="14d" size="small">{{ t('healthScore.trend.range.14d') }}</v-btn>
        <v-btn value="30d" size="small">{{ t('healthScore.trend.range.30d') }}</v-btn>
      </v-btn-toggle>
    </v-card-title>
    <v-card-text>
      <v-skeleton-loader v-if="loading" type="image" height="300" />
      <v-alert v-else-if="unavailable" type="info" variant="tonal" density="compact">
        {{ t('healthScore.trend.unavailable') }}
      </v-alert>
      <div v-else-if="chartData" class="trend-chart">
        <Line :data="chartData" :options="chartOptions as any" />
      </div>
      <v-alert v-else type="info" variant="tonal" density="compact">
        {{ t('healthScore.trend.noData') }}
      </v-alert>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.trend-chart {
  width: 100%;
  height: 300px;
}
</style>
