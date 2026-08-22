<script setup lang="ts">
import { computed } from 'vue'
import { Doughnut } from 'vue-chartjs'
import { ArcElement, Chart as ChartJS, Legend, Tooltip } from 'chart.js'
import { useI18n } from 'vue-i18n'

import { CONTEXT_COLORS, hitRatio, sumBy, sumValues, type MatrixRow } from './types'
import { fmtCompact, fmtPct } from '@/utils/format'

ChartJS.register(ArcElement, Tooltip, Legend)

const props = defineProps<{ rows: MatrixRow[] }>()

const { t } = useI18n()

const hits = computed(() => sumValues(props.rows, 'hits'))
const reads = computed(() => sumValues(props.rows, 'reads'))
const overall = computed(() => hitRatio(props.rows))

const perContext = computed(() =>
  [...sumBy(props.rows, (r) => r.context)]
    .map(([context, rows]) => ({
      context,
      ratio: hitRatio(rows),
      accesses: sumValues(rows, 'hits') + sumValues(rows, 'reads'),
    }))
    .filter((c) => c.accesses > 0)
    .sort((a, b) => b.accesses - a.accesses),
)

const chartData = computed(() => {
  if (overall.value === null) return null

  return {
    labels: [t('io.cache.hits'), t('io.cache.reads')],
    datasets: [
      {
        data: [hits.value, reads.value],
        backgroundColor: ['#4CAF50', '#EF5350'],
        borderWidth: 0,
      },
    ],
  }
})

const chartOptions = {
  responsive: true,
  maintainAspectRatio: false,
  cutout: '65%',
  plugins: { legend: { display: true, position: 'bottom' as const } },
}

function ratioColor(ratio: number | null): string {
  if (ratio === null) return 'grey'
  if (ratio >= 0.99) return 'success'
  if (ratio >= 0.95) return 'warning'
  return 'error'
}
</script>

<template>
  <v-card class="mb-4 h-100">
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-memory" />
      {{ t('io.cache.title') }}
      <v-icon size="small" icon="mdi-help-circle-outline" class="ms-1 text-medium-emphasis">
        <v-tooltip activator="parent" location="bottom" max-width="360">
          {{ t('io.cache.hint') }}
        </v-tooltip>
      </v-icon>
    </v-card-title>
    <v-card-text>
      <template v-if="chartData">
        <div class="io-donut">
          <Doughnut :data="chartData" :options="chartOptions" />
        </div>

        <div class="text-center text-h6 mb-3">
          {{ fmtPct((overall ?? 0) * 100, 2) }}
          <div class="text-caption text-medium-emphasis">
            {{ t('io.cache.subtitle', { hits: fmtCompact(hits), reads: fmtCompact(reads) }) }}
          </div>
        </div>

        <div v-for="c in perContext" :key="c.context" class="mb-2">
          <div class="d-flex justify-space-between text-caption">
            <span>
              <v-icon size="x-small" icon="mdi-circle" :color="CONTEXT_COLORS[c.context]" class="me-1" />
              {{ c.context }}
            </span>
            <span>{{ fmtPct((c.ratio ?? 0) * 100, 2) }}</span>
          </div>
          <v-progress-linear
            :model-value="(c.ratio ?? 0) * 100"
            :color="ratioColor(c.ratio)"
            height="6"
            rounded
          />
        </div>
      </template>

      <v-alert v-else type="info" variant="tonal" density="compact">
        {{ t('io.cache.noAccess') }}
      </v-alert>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.io-donut {
  width: 100%;
  height: 160px;
}
</style>
