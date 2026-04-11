<script setup lang="ts">
import { computed } from 'vue'
import { Doughnut } from 'vue-chartjs'
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js'
import { useI18n } from 'vue-i18n'
import { getConnectionStates } from '@/api/gen/default/default'
import type { ConnectionState } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'

ChartJS.register(ArcElement, Tooltip, Legend)

const STATE_COLORS: Record<string, string> = {
  active: '#4CAF50',
  idle: '#2196F3',
  'idle in transaction': '#FF9800',
  'idle in transaction (aborted)': '#F44336',
  'fastpath function call': '#9C27B0',
  disabled: '#607D8B',
}
const FALLBACK_COLOR = '#795548'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()

const headers = computed(() => [
  { title: t('header.state'), key: 'State' },
  { title: t('header.amount'), key: 'Count' },
])

const { items, loading } = useApiLoader<ConnectionState[]>(
  () => getConnectionStates({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError,
  },
)

const totalConnections = computed(() =>
  items.value.reduce((sum, s) => sum + s.Count, 0),
)

const chartItems = computed(() => items.value.filter(s => s.Count > 0))

const chartData = computed(() => ({
  labels: chartItems.value.map(s => s.State),
  datasets: [{
    data: chartItems.value.map(s => s.Count),
    backgroundColor: chartItems.value.map(s => STATE_COLORS[s.State] || FALLBACK_COLOR),
    hoverOffset: 4,
  }],
}))

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (ctx: { label: string; parsed: number; dataset: { data: number[] } }) => {
          const total = ctx.dataset.data.reduce((a: number, b: number) => a + b, 0)
          const pct = ((ctx.parsed / total) * 100).toFixed(1)
          return `${ctx.label}: ${ctx.parsed} (${pct}%)`
        },
      },
    },
  },
}))
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-2">
      <v-icon start icon="mdi-connection" />{{ t('home.connectionStates') }}
      <v-chip v-if="!loading" size="small" variant="tonal">
        {{ t('home.totalConnections') }}: {{ totalConnections }}
      </v-chip>
    </v-card-title>
    <v-card-text>
      <v-row align="start">
        <v-col cols="12" md="8">
          <v-data-table
            :headers="headers"
            :items="items"
            :loading="loading"
          />
        </v-col>
        <v-col v-if="!loading && chartItems.length" cols="12" md="4">
          <div class="chart-container">
            <Doughnut :data="chartData" :options="chartOptions as any" />
          </div>
        </v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.chart-container {
  width: 100%;
  max-width: 280px;
  height: 200px;
}
</style>
