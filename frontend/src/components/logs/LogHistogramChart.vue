<script setup lang="ts">
import { computed } from 'vue'
import { Bar } from 'vue-chartjs'
import { Chart as ChartJS, CategoryScale, LinearScale, BarElement, Tooltip, Legend } from 'chart.js'
import zoomPlugin from 'chartjs-plugin-zoom'
import { useI18n } from 'vue-i18n'
import { useThemeStore } from '@/stores/theme'
import type { LogEntry } from '@/api/models'
import { fmtChartTime, fmtDateTime } from '@/utils/format'

ChartJS.register(CategoryScale, LinearScale, BarElement, Tooltip, Legend, zoomPlugin)

const props = defineProps<{
  items: LogEntry[]
}>()

const emit = defineEmits<{
  // Click on a bucket narrows the search period to that bucket.
  zoom: [fromIso: string, toIso: string]
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
  return fmtChartTime(ms, spanMs >= DAY_MS)
}

// Bucket geometry, shared by the chart data and the click-to-zoom handler.
const bucketMeta = computed(() => {
  const pts = parsed.value
  if (!pts.length) return null
  const min = Math.min(...pts.map(p => p.timeMs))
  const max = Math.max(...pts.map(p => p.timeMs))
  const span = max - min
  const width = Math.max(1000, Math.ceil((span + 1) / MAX_BUCKETS))
  const buckets = Math.max(1, Math.ceil((span + 1) / width))
  return { min, max, span, width, buckets }
})

// Loaded records bucketed over the time span they actually cover (not the
// requested from..to), stacked by severity.
const chartData = computed(() => {
  const meta = bucketMeta.value
  if (!meta) return null
  const pts = parsed.value
  const { min, span, width, buckets } = meta

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

// Drag-select fires both onZoomComplete and (on mouseup) a chart click; the
// timestamp guard keeps the trailing click from re-zooming to a single bucket.
let lastDragZoomAt = 0

function onBarClick(_evt: unknown, elements: Array<{ index: number }>) {
  const meta = bucketMeta.value
  if (!meta || !elements.length) return
  if (Date.now() - lastDragZoomAt < 500) return
  const start = meta.min + elements[0].index * meta.width
  emit('zoom', new Date(start).toISOString(), new Date(start + meta.width).toISOString())
}

// The x axis is a category scale, so after a drag-zoom scale min/max are
// bucket indices; map them back to time and hand the range to the filters.
// Emit comes before resetZoom so a reset quirk can't swallow the selection.
function onDragZoom(ctx: { chart: { scales: { x: { min: unknown; max: unknown } }; resetZoom?: (mode?: string) => void } }) {
  const meta = bucketMeta.value
  if (!meta) return

  // Attached to both onZoom and onZoomComplete (plugin versions differ in
  // which one fires for drag); the window also swallows the event that
  // resetZoom below emits with the restored full scale.
  if (Date.now() - lastDragZoomAt < 300) return

  const lo = Number(ctx.chart.scales?.x?.min)
  const hi = Number(ctx.chart.scales?.x?.max)
  if (!Number.isFinite(lo) || !Number.isFinite(hi)) return

  const loIdx = Math.max(0, Math.floor(lo))
  const hiIdx = Math.min(meta.buckets - 1, Math.ceil(hi))
  if (hiIdx < loIdx) return

  lastDragZoomAt = Date.now()
  emit(
    'zoom',
    new Date(meta.min + loIdx * meta.width).toISOString(),
    new Date(meta.min + (hiIdx + 1) * meta.width).toISOString(),
  )

  // The chart itself stays unzoomed — the range now lives in the filters and
  // the next search redraws it over the narrowed period.
  try {
    ctx.chart.resetZoom?.('none')
  } catch {
    // Best-effort: a zoomed chart is cosmetic, the filters already got the range.
  }
}

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: 'index' as const, intersect: false },
  onClick: onBarClick,
  plugins: {
    legend: {
      display: presentSeverities.value.length > 1,
      position: 'bottom' as const,
    },
    tooltip: {
      filter: (item: { raw: unknown }) => item.raw !== 0,
    },
    zoom: {
      zoom: {
        // Drag-select only: wheel/pinch zoom would hijack page scrolling.
        drag: { enabled: true, backgroundColor: 'rgba(128, 160, 255, 0.2)' },
        wheel: { enabled: false },
        pinch: { enabled: false },
        mode: 'x' as const,
        onZoom: onDragZoom,
        onZoomComplete: onDragZoom,
      },
      pan: { enabled: false },
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
  const base = t('logs.chart.coverage', { count: pts.length, from: fmtDateTime(min), to: fmtDateTime(max) })
  return `${base} · ${t('logs.chart.zoomHint')}`
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
