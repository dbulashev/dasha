<script setup lang="ts">
import { computed } from 'vue'
import { Bar } from 'vue-chartjs'
import { BarElement, CategoryScale, Chart as ChartJS, Legend, LinearScale, Tooltip } from 'chart.js'
import zoomPlugin from 'chartjs-plugin-zoom'

import { useThemeStore } from '@/stores/theme'
import { fmtClockTime } from '@/utils/format'
import type { ChartData } from './activityBuffer'

ChartJS.register(CategoryScale, LinearScale, BarElement, Tooltip, Legend, zoomPlugin)

const props = defineProps<{
  data: ChartData
  label: (key: string) => string
  color: (key: string, dark: boolean) => string
  hatch?: (key: string, dark: boolean) => string | null
}>()

const emit = defineEmits<{
  select: [from: number, to: number]
}>()

const themeStore = useThemeStore()
const isDark = computed(() => themeStore.currentTheme() === 'dark')

const patterns = new Map<string, CanvasPattern | string>()

// 45° stripes of `line` over `base`, tiled; falls back to the plain color without a 2D canvas.
function hatchPattern(base: string, line: string): CanvasPattern | string {
  const key = `${base}|${line}`
  const cached = patterns.get(key)
  if (cached) return cached

  const size = 6
  const tile = document.createElement('canvas')
  tile.width = size
  tile.height = size
  const ctx = tile.getContext('2d')
  const pattern = ctx && fillTile(ctx, size, base, line)
  const result = pattern ?? base
  patterns.set(key, result)
  return result
}

function fillTile(ctx: CanvasRenderingContext2D, size: number, base: string, line: string): CanvasPattern | null {
  ctx.fillStyle = base
  ctx.fillRect(0, 0, size, size)
  ctx.strokeStyle = line
  ctx.lineWidth = 2
  ctx.beginPath()
  for (const o of [-size, 0, size]) {
    ctx.moveTo(o, size)
    ctx.lineTo(o + size, 0)
  }
  ctx.stroke()
  return ctx.createPattern(ctx.canvas, 'repeat')
}

function fill(key: string): CanvasPattern | string {
  const base = props.color(key, isDark.value)
  const line = props.hatch?.(key, isDark.value)
  return line ? hatchPattern(base, line) : base
}

const chartData = computed(() => ({
  labels: props.data.starts.map((ms) => fmtClockTime(ms)),
  datasets: props.data.series.map((s) => ({
    label: props.label(s.key),
    data: s.data,
    backgroundColor: fill(s.key),
    borderWidth: 0,
    barPercentage: 1,
    categoryPercentage: 0.95,
    stack: 'activity',
  })),
}))

function emitBars(lo: number, hi: number) {
  const { starts, stepMs } = props.data
  const a = Math.max(0, Math.min(lo, hi))
  const b = Math.min(starts.length - 1, Math.max(lo, hi))
  if (b < a || !starts.length) return
  emit('select', starts[a], starts[b] + stepMs)
}

// Drag-select fires the zoom callback and then a click; the guard drops the click.
let lastDragAt = 0

function onBarClick(_evt: unknown, elements: Array<{ index: number }>) {
  if (!elements.length || Date.now() - lastDragAt < 500) return
  emitBars(elements[0].index, elements[0].index)
}

function onDragZoom(ctx: {
  chart: { scales: { x: { min: unknown; max: unknown } }; resetZoom?: (mode?: string) => void }
}) {
  if (Date.now() - lastDragAt < 300) return

  const lo = Number(ctx.chart.scales?.x?.min)
  const hi = Number(ctx.chart.scales?.x?.max)
  if (!Number.isFinite(lo) || !Number.isFinite(hi)) return

  lastDragAt = Date.now()
  emitBars(Math.floor(lo), Math.ceil(hi))

  try {
    ctx.chart.resetZoom?.('none')
  } catch {
    // Best effort: the selection has already been emitted.
  }
}

const ink = computed(() =>
  isDark.value
    ? { text: 'rgba(255, 255, 255, 0.87)', muted: 'rgba(255, 255, 255, 0.6)', grid: 'rgba(255, 255, 255, 0.08)' }
    : { text: 'rgba(0, 0, 0, 0.87)', muted: 'rgba(0, 0, 0, 0.6)', grid: 'rgba(0, 0, 0, 0.08)' },
)

function hasData(datasetIndex: number): boolean {
  return props.data.series[datasetIndex]?.data.some((v) => v !== null && v > 0) ?? false
}

const options = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: false as const,
  interaction: { mode: 'index' as const, intersect: false },
  onClick: onBarClick,
  plugins: {
    legend: {
      position: 'bottom' as const,
      labels: {
        color: ink.value.text,
        boxWidth: 12,
        boxHeight: 12,
        padding: 14,
        filter: (item: { datasetIndex?: number }) => hasData(item.datasetIndex ?? -1),
      },
    },
    tooltip: {
      filter: (item: { raw: unknown }) => typeof item.raw === 'number' && item.raw > 0,
      callbacks: {
        label: (item: { dataset: { label?: string }; raw: unknown }) =>
          `${item.dataset.label}: ${Number(item.raw).toFixed(2)}`,
      },
    },
    zoom: {
      zoom: {
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
    x: {
      stacked: true,
      ticks: { maxTicksLimit: 10, maxRotation: 0, color: ink.value.muted },
      grid: { display: false },
      border: { color: ink.value.grid },
    },
    y: {
      stacked: true,
      beginAtZero: true,
      ticks: { color: ink.value.muted },
      grid: { color: ink.value.grid },
      border: { display: false },
    },
  },
}))
</script>

<template>
  <div class="activity-chart">
    <Bar :data="chartData" :options="options as any" />
  </div>
</template>

<style scoped>
.activity-chart {
  width: 100%;
  height: 280px;
}
</style>
