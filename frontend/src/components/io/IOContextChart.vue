<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Line } from 'vue-chartjs'
import {
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LineElement,
  LinearScale,
  PointElement,
  Tooltip,
} from 'chart.js'
import { useI18n } from 'vue-i18n'

import type { IOHistory } from '@/api/models/index'
import { CONTEXT_COLORS, type MetricMode } from './types'
import { fmtChartTime } from '@/utils/format'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Filler, Tooltip, Legend)

const props = defineProps<{
  history: IOHistory | null
  metricMode: MetricMode
  object: string
  loading: boolean
}>()

const { t } = useI18n()

const COUNT_CHOICES = ['reads', 'hits', 'writes', 'extends']
const TIME_CHOICES = ['read_time', 'write_time', 'extend_time', 'fsync_time']

const metric = ref('reads')

watch(
  () => props.metricMode,
  (mode) => {
    metric.value = mode === 'count' ? 'reads' : 'read_time'
  },
  { immediate: true },
)

const choices = computed(() => (props.metricMode === 'count' ? COUNT_CHOICES : TIME_CHOICES))

const spansDays = computed(() => {
  const series = props.history?.series ?? []
  const points = series[0]?.points ?? []
  if (points.length < 2) return false
  const span = Date.parse(points[points.length - 1].to) - Date.parse(points[0].from)
  return span > 24 * 3600 * 1000
})

const chartData = computed(() => {
  const series = props.history?.series ?? []
  const reference = series[0]?.points ?? []
  if (!reference.length) return null

  return {
    labels: reference.map((p) => fmtChartTime(p.to, spansDays.value)),
    datasets: series.map((s) => {
      const context = s.key.context ?? '—'
      const color = CONTEXT_COLORS[context] ?? '#78909C'

      return {
        label: context,
        // A broken epoch is a gap, not a zero: chart.js skips null points.
        data: s.points.map((p) => {
          if (!p.complete || p.duration_seconds <= 0) return null
          const value = p.values[metric.value] ?? 0
          return props.metricMode === 'count'
            ? value / p.duration_seconds
            : value / (p.duration_seconds * 1000)
        }),
        borderColor: color,
        backgroundColor: `${color}55`,
        fill: 'origin' as const,
        pointRadius: 0,
        borderWidth: 1.5,
        tension: 0.2,
      }
    }),
  }
})

const axisTitle = computed(() =>
  props.metricMode === 'count' ? t('io.chart.perSecond') : t('io.chart.timeShare'),
)

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: 'index' as const, intersect: false },
  plugins: { legend: { display: true, position: 'bottom' as const } },
  scales: {
    x: { stacked: true, ticks: { maxTicksLimit: 10, maxRotation: 0 } },
    y: { stacked: true, beginAtZero: true, title: { display: true, text: axisTitle.value } },
  },
}))
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <v-icon start icon="mdi-chart-areaspline" />
      {{ t('io.chart.title', { object }) }}
      <v-icon size="small" icon="mdi-help-circle-outline" class="ms-1 text-medium-emphasis">
        <v-tooltip activator="parent" location="bottom" max-width="360">
          {{ t('io.chart.hint') }}
        </v-tooltip>
      </v-icon>
      <v-spacer />
      <v-btn-toggle v-model="metric" density="compact" variant="outlined" mandatory>
        <v-btn v-for="m in choices" :key="m" :value="m" size="small">{{ m }}</v-btn>
      </v-btn-toggle>
    </v-card-title>
    <v-card-text>
      <v-skeleton-loader v-if="loading" type="image" height="300" />
      <div v-else-if="chartData" class="io-chart">
        <Line :data="chartData" :options="chartOptions as any" />
      </div>
      <v-alert v-else type="info" variant="tonal" density="compact">
        {{ t('io.noData') }}
      </v-alert>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.io-chart {
  width: 100%;
  height: 320px;
}
</style>
