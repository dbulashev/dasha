<script setup lang="ts">
import { computed } from 'vue'
import { Doughnut } from 'vue-chartjs'
import { Chart as ChartJS, ArcElement, Tooltip } from 'chart.js'
import { useI18n } from 'vue-i18n'
import { useThemeStore } from '@/stores/theme'
import { getMaintenanceAutovacuumSummary } from '@/api/gen/default/default'
import type { MaintenanceAutovacuumSummary } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'

ChartJS.register(ArcElement, Tooltip)

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()
const themeStore = useThemeStore()

const { items: summary, loading } = useApiLoader<MaintenanceAutovacuumSummary | null>(
  () => getMaintenanceAutovacuumSummary({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
  }),
  {
    deps: [clusterName, hostName, databaseName],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError,
    defaultValue: null,
  },
)

// customFetch can synthesize an empty object on a bodiless response, so
// numeric fields are coerced instead of trusted (undefined would render NaN).
const num = (v: unknown): number => (typeof v === 'number' && Number.isFinite(v) ? v : 0)

const hasData = computed(() => typeof summary.value?.TablesTotal === 'number')

// CVD-validated categorical slots (blue/aqua/yellow), stepped per theme surface.
const SLICE_COLORS: Record<'light' | 'dark', string[]> = {
  light: ['#2a78d6', '#1baf7a', '#eda100'],
  dark: ['#3987e5', '#199e70', '#c98500'],
}

const isDark = computed(() => themeStore.currentTheme() === 'dark')
// Vuetify default surfaces, same convention as LogHistogramChart.
const surface = computed(() => (isDark.value ? '#212121' : '#FFFFFF'))

const slices = computed(() => {
  const s = summary.value
  if (!s || !hasData.value) return []
  const colors = SLICE_COLORS[isDark.value ? 'dark' : 'light']
  return [
    { key: 'dueVacuum', label: t('maintenance.summary.dueVacuum'), value: num(s.TablesDueVacuumOnly), color: colors[0] },
    { key: 'dueAnalyze', label: t('maintenance.summary.dueAnalyze'), value: num(s.TablesDueAnalyzeOnly), color: colors[1] },
    { key: 'dueBoth', label: t('maintenance.summary.dueBoth'), value: num(s.TablesDueBoth), color: colors[2] },
  ]
})

const dueTotal = computed(() => slices.value.reduce((sum, s) => sum + s.value, 0))

const chartSlices = computed(() => slices.value.filter(s => s.value > 0))

const chartData = computed(() => ({
  labels: chartSlices.value.map(s => s.label),
  datasets: [{
    data: chartSlices.value.map(s => s.value),
    backgroundColor: chartSlices.value.map(s => s.color),
    // 2px surface-colored gap so adjacent segments never touch
    borderWidth: 2,
    borderColor: surface.value,
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
          const pct = total ? ((ctx.parsed / total) * 100).toFixed(1) : '0'
          return `${ctx.label}: ${ctx.parsed} (${pct}%)`
        },
      },
    },
  },
}))

const pct = (v: number) => dueTotal.value ? `${((v / dueTotal.value) * 100).toFixed(1)}%` : ''
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <v-icon start icon="mdi-chart-pie" /><span>{{ t('maintenance.summary.title') }}</span>
      <v-tooltip :text="t('maintenance.summary.hint')" location="bottom" max-width="420">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
      <v-chip v-if="hasData" size="small" variant="tonal">
        {{ t('maintenance.summary.due') }}: {{ dueTotal }} / {{ num(summary!.TablesTotal) }}
      </v-chip>
    </v-card-title>
    <v-card-text>
      <v-progress-linear v-if="loading" indeterminate />
      <v-row v-else-if="hasData" align="center">
        <v-col cols="12" :sm="dueTotal ? 4 : 8" class="d-flex justify-center">
          <div v-if="chartSlices.length" class="chart-container">
            <Doughnut :data="chartData" :options="chartOptions as any" />
          </div>
          <v-alert v-else type="success" variant="tonal" density="compact">
            {{ t('maintenance.summary.allOk') }}
          </v-alert>
        </v-col>
        <v-col v-if="dueTotal" cols="12" sm="4">
          <div v-for="s in slices" :key="s.key" class="d-flex align-center ga-2 mb-2">
            <span class="legend-dot flex-shrink-0" :style="{ backgroundColor: s.color }" />
            <span class="text-body-2">{{ s.label }}</span>
            <v-spacer />
            <span class="text-body-2 font-weight-medium">{{ s.value }}</span>
            <span v-if="s.value" class="text-caption text-medium-emphasis">({{ pct(s.value) }})</span>
          </div>
          <v-divider class="my-2" />
          <div class="d-flex align-center ga-2">
            <span class="text-body-2 text-medium-emphasis">{{ t('maintenance.summary.ok') }}</span>
            <v-spacer />
            <span class="text-body-2">{{ num(summary!.TablesTotal) - dueTotal }}</span>
          </div>
        </v-col>
        <v-col cols="12" sm="4">
          <div class="d-flex ga-6 justify-center">
            <div class="text-center">
              <div class="text-h4">{{ num(summary!.RunningVacuums) }}</div>
              <div class="text-caption text-medium-emphasis">{{ t('maintenance.summary.runningVacuums') }}</div>
            </div>
            <div class="text-center">
              <div class="text-h4">{{ num(summary!.RunningAnalyzes) }}</div>
              <div class="text-caption text-medium-emphasis">{{ t('maintenance.summary.runningAnalyzes') }}</div>
            </div>
          </div>
        </v-col>
      </v-row>
      <div v-else class="text-medium-emphasis">{{ t('noData') }}</div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.chart-container {
  width: 100%;
  max-width: 240px;
  height: 200px;
}

.legend-dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  display: inline-block;
}
</style>
