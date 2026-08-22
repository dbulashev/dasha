<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { isByteMetric, metricsFor, type MatrixRow, type MetricMode } from './types'
import { fmtBytes, fmtCompact, fmtPct } from '@/utils/format'

const props = defineProps<{
  rows: MatrixRow[]
  metricMode: MetricMode
  showIdle: boolean
  loading: boolean
}>()

const { t } = useI18n()

const metrics = computed(() => metricsFor(props.metricMode))

// Bytes are a second view of the same reads and writes; counting them into the
// share would drown the operation counts they are derived from.
const activityMetrics = computed(() => metrics.value.filter((m) => !isByteMetric(m)))

interface Item extends Record<string, string | number> {
  backend_type: string
  object: string
  context: string
  activity: number
  share: number
}

const items = computed<Item[]>(() => {
  const rows = props.rows.map((r) => {
    const item = {
      backend_type: r.backend_type,
      object: r.object,
      context: r.context,
      activity: 0,
      share: 0,
    } as Item

    for (const m of metrics.value) {
      item[m] = r.values[m] ?? 0
    }

    item.activity = activityMetrics.value.reduce((acc, m) => acc + (r.values[m] ?? 0), 0)

    return item
  })

  const total = rows.reduce((acc, r) => acc + r.activity, 0)

  for (const r of rows) {
    r.share = total > 0 ? (r.activity / total) * 100 : 0
  }

  return props.showIdle ? rows : rows.filter((r) => r.activity > 0)
})

const headers = computed(() => [
  { title: t('io.matrix.backendType'), key: 'backend_type' },
  { title: t('io.matrix.object'), key: 'object' },
  { title: t('io.matrix.context'), key: 'context' },
  ...metrics.value.map((m) => ({ title: m, key: m, align: 'end' as const })),
  { title: t('io.matrix.share'), key: 'share', align: 'end' as const },
])

function cell(metric: string, value: number): string {
  if (!value) return '0'
  if (isByteMetric(metric)) return fmtBytes(value)
  return props.metricMode === 'count' ? fmtCompact(value) : `${fmtCompact(value)} ms`
}
</script>

<template>
  <v-card>
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-table" />
      {{ t('io.matrix.title') }}
      <v-icon size="small" icon="mdi-help-circle-outline" class="ms-1 text-medium-emphasis">
        <v-tooltip activator="parent" location="bottom" max-width="360">
          {{ t('io.matrix.hint') }}
        </v-tooltip>
      </v-icon>
    </v-card-title>
    <v-card-text>
      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
        density="compact"
        :items-per-page="25"
        multi-sort
      >
        <template v-for="m in metrics" #[`item.${m}`]="{ item }" :key="m">
          <span :class="{ 'text-disabled': !item[m] }">{{ cell(m, item[m] as number) }}</span>
        </template>
        <template #item.share="{ item }">
          {{ fmtPct(item.share) }}
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
