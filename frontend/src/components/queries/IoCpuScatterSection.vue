<script setup lang="ts">
import { computed, ref } from 'vue'
import { Scatter } from 'vue-chartjs'
import {
  Chart as ChartJS,
  LinearScale,
  PointElement,
  Tooltip,
} from 'chart.js'
import { useI18n } from 'vue-i18n'
import { getQueriesReport } from '@/api/gen/default/default'
import type { QueryReport } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import { copyToClipboard } from '@/utils/sql'
import { fmtMs, pickTimeScale, fmtScaled } from '@/utils/format'

ChartJS.register(LinearScale, PointElement, Tooltip)

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()

const snackbar = ref(false)
const copiedQueryId = ref('')

const { items, loading } = useApiLoader<QueryReport[]>(
  () => getQueriesReport({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError,
  },
)

const points = computed(() =>
  items.value
    .filter(r => r.IoTimeMs != null && r.CpuTimeMs != null)
    .map(r => ({
      x: r.IoTimeMs!,
      y: r.CpuTimeMs!,
      calls: r.Calls ?? 0,
      queryId: r.QueryID,
    })),
)

const maxCalls = computed(() =>
  Math.max(...points.value.map(p => p.calls), 1),
)

const xScale = computed(() => pickTimeScale(Math.max(...points.value.map(p => p.x), 0)))
const yScale = computed(() => pickTimeScale(Math.max(...points.value.map(p => p.y), 0)))

const scatterData = computed(() => ({
  datasets: [{
    data: points.value,
    backgroundColor: 'rgba(33, 150, 243, 0.6)',
    borderColor: 'rgba(33, 150, 243, 1)',
    borderWidth: 1,
    pointRadius: points.value.map(p => 4 + 16 * Math.sqrt(p.calls / maxCalls.value)),
    pointHoverRadius: points.value.map(p => 6 + 16 * Math.sqrt(p.calls / maxCalls.value)),
  }],
}))

function onChartClick(_event: unknown, elements: { index: number }[]) {
  if (!elements.length || !elements[0]) return
  const point = points.value[elements[0].index]
  if (point) {
    copiedQueryId.value = String(point.queryId)
    copyToClipboard(copiedQueryId.value)
    snackbar.value = true
  }
}

function onChartHover(event: { native: MouseEvent | null }, elements: unknown[]) {
  const canvas = event.native?.target as HTMLCanvasElement | null
  if (canvas) {
    canvas.style.cursor = elements.length ? 'pointer' : 'default'
  }
}

const scatterOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  onClick: onChartClick,
  onHover: onChartHover,
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (ctx: { raw: { x: number; y: number; calls: number; queryId: number } }) => {
          const p = ctx.raw
          return [
            `Query ${p.queryId}`,
            `IO: ${fmtMs(p.x, t)}`,
            `CPU: ${fmtMs(p.y, t)}`,
            `Calls: ${p.calls.toLocaleString()}`,
          ]
        },
        afterBody: () => t('chart.clickToCopy'),
      },
    },
  },
  scales: {
    x: {
      title: { display: true, text: `${t('chart.ioTime')} (${t('time.' + xScale.value.unit)})` },
      beginAtZero: true,
      ticks: {
        callback: (value: number) => fmtScaled(value, xScale.value),
      },
    },
    y: {
      title: { display: true, text: `${t('chart.cpuTime')} (${t('time.' + yScale.value.unit)})` },
      beginAtZero: true,
      ticks: {
        callback: (value: number) => fmtScaled(value, yScale.value),
      },
    },
  },
}))
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-chart-scatter-plot" />{{ t('IO vs CPU Time') }}
    </v-card-title>
    <v-card-text>
      <v-skeleton-loader v-if="loading" type="image" height="360" />
      <div v-else-if="points.length" class="chart-container">
        <Scatter :data="scatterData" :options="scatterOptions as any" />
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
  height: 360px;
}
</style>
