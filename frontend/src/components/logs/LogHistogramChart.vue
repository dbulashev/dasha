<script setup lang="ts">
import { computed } from 'vue'
import { Bar } from 'vue-chartjs'
import { Chart as ChartJS, CategoryScale, LinearScale, BarElement, Tooltip, Legend } from 'chart.js'
import { useI18n } from 'vue-i18n'
import { useThemeStore } from '@/stores/theme'
import type { LogEntry } from '@/api/models'
import { fmtDateTime } from '@/utils/format'

ChartJS.register(CategoryScale, LinearScale, BarElement, Tooltip, Legend)

const props = defineProps<{
  items: LogEntry[]
}>()

const { t } = useI18n()
const themeStore = useThemeStore()

// Stack order = severity rank; lowercase keys cover both PostgreSQL (UPPER)
// and pooler (lower) spellings. Each palette is CVD-validated against the
// corresponding surface, so the two modes use different steps of the same hues.
const SEVERITY_ORDER = ['debug', 'log', 'info', 'notice', 'warning', 'error', 'fatal', 'panic']

const COLORS_LIGHT: Record<string, string> = {
  debug: '#A0522D',
  log: '#42A5F5',
  info: '#00897B',
  notice: '#3949AB',
  warning: '#FB8C00',
  error: '#E53935',
  fatal: '#AB47BC',
  panic: '#7B1FA2',
}

const COLORS_DARK: Record<string, string> = {
  debug: '#B25E33',
  log: '#2196F3',
  info: '#009688',
  notice: '#5C6BC0',
  warning: '#E8650E',
  error: '#E53935',
  fatal: '#BA68C8',
  panic: '#9C27B0',
}

const FALLBACK_COLOR = '#9E9E9E'
const MAX_BUCKETS = 40
const DAY_MS = 24 * 3600 * 1000

const isDark = computed(() => themeStore.currentTheme() === 'dark')
const colors = computed(() => (isDark.value ? COLORS_DARK : COLORS_LIGHT))
// Vuetify default surfaces — the palettes above were validated against these.
const surface = computed(() => (isDark.value ? '#212121' : '#FFFFFF'))

function rank(key: string): number {
  const i = SEVERITY_ORDER.indexOf(key)
  return i === -1 ? SEVERITY_ORDER.length : i
}

// Severities present in the data, in rank order, keeping the original casing
// for display (label) and the lowercase key for color/rank lookups.
const presentSeverities = computed(() => {
  const byKey = new Map<string, string>()
  for (const e of props.items) {
    const label = e.severity || '?'
    const key = label.toLowerCase()
    if (!byKey.has(key)) byKey.set(key, label)
  }
  return [...byKey.entries()]
    .map(([key, label]) => ({ key, label }))
    .sort((a, b) => rank(a.key) - rank(b.key))
})

const parsed = computed(() =>
  props.items
    .map(e => ({ timeMs: Date.parse(e.timestamp), key: (e.severity || '?').toLowerCase() }))
    .filter(p => Number.isFinite(p.timeMs)),
)

function bucketLabel(ms: number, spanMs: number): string {
  const d = new Date(ms)
  return spanMs < DAY_MS
    ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : d.toLocaleString([], { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

// Loaded records bucketed over the time span they actually cover (not the
// requested from..to), stacked by severity.
const chartData = computed(() => {
  const pts = parsed.value
  if (!pts.length) return null

  const min = Math.min(...pts.map(p => p.timeMs))
  const max = Math.max(...pts.map(p => p.timeMs))
  const span = max - min
  const width = Math.max(1000, Math.ceil((span + 1) / MAX_BUCKETS))
  const buckets = Math.max(1, Math.ceil((span + 1) / width))

  const counts = new Map<string, number[]>()
  for (const { key } of presentSeverities.value) counts.set(key, new Array(buckets).fill(0))
  for (const p of pts) {
    const idx = Math.min(buckets - 1, Math.floor((p.timeMs - min) / width))
    counts.get(p.key)![idx]++
  }

  return {
    labels: Array.from({ length: buckets }, (_, i) => bucketLabel(min + i * width, span)),
    datasets: presentSeverities.value.map(({ key, label }) => ({
      label,
      data: counts.get(key)!,
      backgroundColor: colors.value[key] ?? FALLBACK_COLOR,
      borderColor: surface.value,
      borderWidth: 1,
      stack: 'logs',
    })),
  }
})

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: 'index' as const, intersect: false },
  plugins: {
    legend: {
      display: presentSeverities.value.length > 1,
      position: 'bottom' as const,
    },
    tooltip: {
      filter: (item: { raw: unknown }) => item.raw !== 0,
    },
  },
  scales: {
    x: { stacked: true, ticks: { maxTicksLimit: 10, maxRotation: 0 } },
    y: { stacked: true, beginAtZero: true, ticks: { precision: 0 } },
  },
}))

const coverage = computed(() => {
  const pts = parsed.value
  if (!pts.length) return ''
  const min = new Date(Math.min(...pts.map(p => p.timeMs))).toISOString()
  const max = new Date(Math.max(...pts.map(p => p.timeMs))).toISOString()
  return t('logs.chart.coverage', { count: pts.length, from: fmtDateTime(min), to: fmtDateTime(max) })
})
</script>

<template>
  <v-card v-if="chartData" class="mb-4">
    <v-card-title class="d-flex align-center">
      <v-icon start icon="mdi-chart-bar" />
      {{ t('logs.chart.title') }}
    </v-card-title>
    <v-card-subtitle>{{ coverage }}</v-card-subtitle>
    <v-card-text>
      <div class="log-histogram">
        <Bar :data="chartData" :options="chartOptions as any" />
      </div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.log-histogram {
  width: 100%;
  height: 220px;
}
</style>
