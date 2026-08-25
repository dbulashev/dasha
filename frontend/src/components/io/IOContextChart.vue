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
import { backendTypeColor, contextColor, neutralColor, type MetricMode } from './types'
import { useThemeStore } from '@/stores/theme'
import { fmtChartTime, fmtCompact, fmtScaled, pickTimeScale } from '@/utils/format'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Filler, Tooltip, Legend)

const props = defineProps<{
  history: IOHistory | null
  metricMode: MetricMode
  objects: string[]
  loading: boolean
}>()

// The series are grouped by context, so the chart draws one object at a time.
const object = defineModel<string>('object', { required: true })
const groupBy = defineModel<'context' | 'backend_type'>('groupBy', { required: true })

const { t, te } = useI18n()
const themeStore = useThemeStore()
const isDark = computed(() => themeStore.currentTheme() === 'dark')

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

interface Series {
  label: string
  color: string | null
  data: (number | null)[]
}

// Counts become operations per second, times milliseconds of I/O per second.
const rawSeries = computed<Series[]>(() =>
  (props.history?.series ?? []).map((s) => {
    const label = (groupBy.value === 'context' ? s.key.context : s.key.backend_type) ?? '—'

    return {
      label,
      color:
        groupBy.value === 'context'
          ? contextColor(label, isDark.value)
          : backendTypeColor(label, isDark.value),
      // A broken epoch is a gap, not a zero: chart.js skips null points.
      data: s.points.map((p) =>
        !p.complete || p.duration_seconds <= 0
          ? null
          : (p.values[metric.value] ?? 0) / p.duration_seconds,
      ),
    }
  }),
)

// A backend type without a slot folds into "other"; a lone one keeps its name.
const series = computed<Series[]>(() => {
  const named = rawSeries.value.filter((s) => s.color !== null)
  const rest = rawSeries.value.filter((s) => s.color === null)
  if (!rest.length) return named
  if (rest.length === 1) return [...named, { ...rest[0], color: neutralColor(isDark.value) }]

  const merged: (number | null)[] = []
  for (const s of rest) {
    s.data.forEach((value, i) => {
      if (value === null) {
        if (merged[i] === undefined) merged[i] = null
        return
      }
      merged[i] = (merged[i] ?? 0) + value
    })
  }

  return [
    ...named,
    {
      label: t('io.chart.otherSeries', { n: rest.length }),
      color: neutralColor(isDark.value),
      data: merged,
    },
  ]
})

const datasets = computed(() =>
  series.value.map((s) => ({
    label: s.label,
    data: s.data,
    borderColor: s.color ?? neutralColor(isDark.value),
    backgroundColor: `${s.color ?? neutralColor(isDark.value)}55`,
    fill: 'origin' as const,
    pointRadius: 0,
    borderWidth: 1.5,
    tension: 0.2,
  })),
)

const peak = computed(() =>
  datasets.value.reduce(
    (acc, d) => d.data.reduce((m: number, v) => (v !== null && v > m ? v : m), acc),
    0,
  ),
)

// Milliseconds per second is a small number; the axis picks a unit, not exponents.
const timeScale = computed(() => pickTimeScale(peak.value))

const chartData = computed(() => {
  const reference = props.history?.series?.[0]?.points ?? []
  if (!reference.length) return null

  return {
    labels: reference.map((p) => fmtChartTime(p.to, spansDays.value)),
    datasets: datasets.value,
  }
})

function fmtValue(value: number): string {
  return props.metricMode === 'count'
    ? fmtCompact(value)
    : fmtScaled(value, timeScale.value)
}

const axisTitle = computed(() =>
  props.metricMode === 'count'
    ? t('io.chart.perSecond')
    : t('io.chart.ioTimePerSecond', { unit: t(`time.${timeScale.value.unit}`) }),
)

const unitSuffix = computed(() =>
  props.metricMode === 'count'
    ? t('io.chart.opsPerSecond')
    : `${t(`time.${timeScale.value.unit}`)}/${t('time.sec')}`,
)

// Context names get a tooltip; backend types already read as English words.
function seriesLabel(label: string): string {
  if (groupBy.value !== 'context') return label
  const key = `io.context.${label}`
  return te(key) ? `${label} — ${t(key)}` : label
}

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: 'index' as const, intersect: false },
  plugins: {
    legend: { display: true, position: 'bottom' as const },
    tooltip: {
      callbacks: {
        title: (items: { label: string }[]) => `${items[0]?.label ?? ''} · ${metric.value}`,
        label: (ctx: { dataset: { label?: string }; parsed: { y: number | null } }) =>
          ctx.parsed.y === null
            ? `${seriesLabel(ctx.dataset.label ?? '')}: ${t('io.partialPoint')}`
            : `${seriesLabel(ctx.dataset.label ?? '')}: ${fmtValue(ctx.parsed.y)} ${unitSuffix.value}`,
        footer: (items: { parsed: { y: number | null } }[]) => {
          const total = items.reduce((acc, i) => acc + (i.parsed.y ?? 0), 0)
          return `${t('io.chart.total')}: ${fmtValue(total)} ${unitSuffix.value}`
        },
      },
    },
  },
  scales: {
    x: { stacked: true, ticks: { maxTicksLimit: 10, maxRotation: 0 } },
    y: {
      stacked: true,
      beginAtZero: true,
      title: { display: true, text: axisTitle.value },
      ticks: { callback: (value: number) => fmtValue(value) },
    },
  },
}))

</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <span class="d-flex align-center ga-1">
        <v-icon start icon="mdi-chart-areaspline" class="flex-shrink-0" />
        <span class="text-wrap">{{ t('io.chart.title') }}</span>
        <v-tooltip :text="t('io.chart.hint')" location="bottom" max-width="360">
          <template #activator="{ props: tip }">
            <v-icon
              v-bind="tip"
              size="small"
              icon="mdi-help-circle-outline"
              class="ms-1 flex-shrink-0 text-medium-emphasis"
            />
          </template>
        </v-tooltip>
      </span>
      <v-spacer />
      <v-btn-toggle v-model="groupBy" density="compact" variant="outlined" mandatory>
        <v-btn value="context" size="small">{{ t('io.chart.groupBy.context') }}</v-btn>
        <v-btn value="backend_type" size="small">{{ t('io.chart.groupBy.backendType') }}</v-btn>
      </v-btn-toggle>
      <v-btn-toggle v-model="object" density="compact" variant="outlined" mandatory>
        <v-btn v-for="o in objects" :key="o" :value="o" size="small">{{ o }}</v-btn>
      </v-btn-toggle>
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
