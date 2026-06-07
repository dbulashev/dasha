<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { Doughnut } from 'vue-chartjs'
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js'
import { useI18n } from 'vue-i18n'
import { getAutosnapshotSummary } from '@/api/gen/default/default'
import type { ClusterSnapshotSummary } from '@/api/models'
import { useViewError } from '@/composables/useViewError'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

ChartJS.register(ArcElement, Tooltip, Legend)

const { t } = useI18n()
const { onError } = useViewError()

// Global, no cluster context — load once on mount (matches the clusters tab).
const items = ref<ClusterSnapshotSummary[]>([])
const loading = ref(false)
const loaded = ref(false)

async function load() {
  loading.value = true
  try {
    items.value = assertOk<ClusterSnapshotSummary[]>(await getAutosnapshotSummary()) ?? []
    loaded.value = true
  } catch (e) {
    // Keep loaded=false so the empty-state alert is not shown on failure
    // (the global error banner explains it).
    onError(getErrorMessage(e), e)
    items.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)

const headers = computed(() => [
  { title: t('autosnapshot.summary.cluster'), key: 'ClusterName' },
  { title: t('autosnapshot.summary.snapshots'), key: 'Snapshots' },
  { title: t('autosnapshot.summary.activitySpike'), key: 'ActivitySpike' },
  { title: t('autosnapshot.summary.roleChange'), key: 'RoleChange' },
  { title: t('autosnapshot.summary.errors'), key: 'Errors' },
])

const totalSnapshots = computed(() => items.value.reduce((s, c) => s + c.Snapshots, 0))

// Deterministic HSL color per cluster name (stable slice colors across reloads).
function colorForCluster(name: string): string {
  let hash = 0
  for (let i = 0; i < name.length; i++) {
    hash = (hash << 5) - hash + name.charCodeAt(i)
    hash |= 0
  }
  return `hsl(${Math.abs(hash) % 360}, 55%, 55%)`
}

const chartItems = computed(() => items.value.filter((c) => c.Snapshots > 0))

const chartData = computed(() => ({
  labels: chartItems.value.map((c) => c.ClusterName),
  datasets: [
    {
      data: chartItems.value.map((c) => c.Snapshots),
      backgroundColor: chartItems.value.map((c) => colorForCluster(c.ClusterName)),
      hoverOffset: 4,
    },
  ],
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
          const pct = total ? ((ctx.parsed / total) * 100).toFixed(1) : '0'
          return `${ctx.label}: ${ctx.parsed} (${pct}%)`
        },
      },
    },
  },
}))
</script>

<template>
  <div>
    <v-alert v-if="loaded && items.length === 0" type="info">
      {{ t('autosnapshot.summary.empty') }}
    </v-alert>

    <v-card v-else>
      <v-card-title class="d-flex align-center ga-2">
        <v-icon start icon="mdi-chart-pie" />{{ t('autosnapshot.summary.title') }}
        <v-chip v-if="!loading" size="small" variant="tonal">
          {{ t('autosnapshot.summary.total') }}: {{ totalSnapshots }}
        </v-chip>
      </v-card-title>
      <v-card-text>
        <v-row align="start">
          <v-col cols="12" md="8">
            <v-data-table
              :headers="headers"
              :items="items"
              :loading="loading"
              density="compact"
            >
              <template #item.ActivitySpike="{ item }">
                <span :class="{ 'text-warning font-weight-medium': item.ActivitySpike > 0 }">
                  {{ item.ActivitySpike }}
                </span>
              </template>
              <template #item.Errors="{ item }">
                <span :class="{ 'text-error font-weight-medium': item.Errors > 0 }">
                  {{ item.Errors }}
                </span>
              </template>
            </v-data-table>
          </v-col>
          <v-col v-if="!loading && chartItems.length" cols="12" md="4">
            <div class="chart-container">
              <Doughnut :data="chartData" :options="chartOptions as any" />
            </div>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>
  </div>
</template>

<style scoped>
.chart-container {
  width: 100%;
  max-width: 280px;
  height: 220px;
  margin: 0 auto;
}
</style>
