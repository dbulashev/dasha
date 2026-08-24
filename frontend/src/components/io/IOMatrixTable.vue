<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  COUNT_METRICS,
  heatColor,
  isByteMetric,
  metricsFor,
  type MatrixRow,
  type MetricMode,
} from './types'
import { useThemeStore } from '@/stores/theme'
import { fmtBytes, fmtCompact, fmtPct } from '@/utils/format'

const props = defineProps<{
  rows: MatrixRow[]
  metricMode: MetricMode
  showIdle: boolean
  loading: boolean
}>()

const { t } = useI18n()
const themeStore = useThemeStore()
const isDark = computed(() => themeStore.currentTheme() === 'dark')

const metrics = computed(() => metricsFor(props.metricMode))

// Objects other than a plain relation ride as a badge on the merged column.
const OBJECT_BADGES: Record<string, { icon: string; hint: string }> = {
  'temp relation': { icon: 'mdi-timer-sand', hint: 'io.matrix.tempObject' },
  wal: { icon: 'mdi-notebook-outline', hint: 'io.matrix.walObject' },
}

// Bytes are a second view of the same reads and writes; the share must not
// count them twice. Share follows the metrics on screen, visibility does not:
// a WAL row can carry I/O and no timings (track_wal_io_timing is separate).
const shareMetrics = computed(() => metrics.value.filter((m) => !isByteMetric(m)))

interface Item extends Record<string, string | number> {
  backend_type: string
  object: string
  context: string
  // Sort key of the merged first column.
  row: string
  activity: number
  weight: number
  share: number
}

const items = computed<Item[]>(() => {
  const rows = props.rows.map((r) => {
    const item = {
      backend_type: r.backend_type,
      object: r.object,
      context: r.context,
      row: `${r.backend_type} ${r.context} ${r.object}`,
      activity: 0,
      weight: 0,
      share: 0,
    } as Item

    for (const m of metrics.value) {
      item[m] = r.values[m] ?? 0
    }

    item.activity = COUNT_METRICS.reduce((acc, m) => acc + (r.values[m] ?? 0), 0)
    item.weight = shareMetrics.value.reduce((acc, m) => acc + (r.values[m] ?? 0), 0)

    return item
  })

  const total = rows.reduce((acc, r) => acc + r.weight, 0)

  for (const r of rows) {
    r.share = total > 0 ? (r.weight / total) * 100 : 0
  }

  return props.showIdle ? rows : rows.filter((r) => r.activity > 0)
})

// Peak per column: a cell's heat compares inside its own metric, not across them.
const peaks = computed(() => {
  const out = new Map<string, number>()
  for (const item of items.value) {
    for (const key of [...metrics.value, 'share']) {
      const value = item[key]
      if (typeof value === 'number') out.set(key, Math.max(out.get(key) ?? 0, value))
    }
  }
  return out
})

function heatProps(column: string) {
  return ({ value }: { value: unknown }) => {
    const peak = peaks.value.get(column) ?? 0
    if (typeof value !== 'number' || value <= 0 || peak <= 0) return {}
    return { style: { backgroundColor: heatColor(value / peak, isDark.value) } }
  }
}

// The three keys and share stay pinned while the metric columns scroll sideways.
const headers = computed(() => [
  // Vuetify derives the sticky left offset from `width` alone, so a pinned
  // column without one lands on top of its neighbour.
  { title: t('io.matrix.row'), key: 'row', fixed: true, width: '320px', minWidth: '320px' },
  {
    title: t('io.matrix.share'),
    key: 'share',
    align: 'end' as const,
    minWidth: '90px',
    cellProps: heatProps('share'),
  },
  ...metrics.value.map((m) => ({
    title: m,
    key: m,
    align: 'end' as const,
    minWidth: '100px',
    cellProps: heatProps(m),
  })),
])

function cell(metric: string, value: number): string {
  if (!value) return ''
  if (isByteMetric(metric)) return fmtBytes(value)
  return props.metricMode === 'count' ? fmtCompact(value) : `${fmtCompact(value)} ms`
}
</script>

<template>
  <v-card>
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-table" class="flex-shrink-0" />
      <span class="text-wrap">{{ t('io.matrix.title') }}</span>
      <v-tooltip :text="t('io.matrix.hint')" location="bottom" max-width="360">
        <template #activator="{ props: tip }">
          <v-icon
            v-bind="tip"
            size="small"
            icon="mdi-help-circle-outline"
            class="ms-1 flex-shrink-0 text-medium-emphasis"
          />
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table
        class="io-matrix"
        :headers="headers"
        :items="items"
        :loading="loading"
        density="compact"
        :items-per-page="25"
        multi-sort
      >
        <template #item.row="{ item }">
          <div class="d-flex align-center ga-1">
            <span>{{ item.backend_type }}</span>
            <v-tooltip
              v-if="OBJECT_BADGES[item.object as string]"
              :text="t(OBJECT_BADGES[item.object as string].hint)"
              location="bottom"
            >
              <template #activator="{ props: tip }">
                <v-icon
                  v-bind="tip"
                  size="x-small"
                  :icon="OBJECT_BADGES[item.object as string].icon"
                  class="text-medium-emphasis"
                />
              </template>
            </v-tooltip>
            <span class="text-medium-emphasis">· {{ item.context }}</span>
          </div>
        </template>

        <template v-for="m in metrics" #[`item.${m}`]="{ item }" :key="m">
          {{ cell(m, item[m] as number) }}
        </template>
        <template #item.share="{ item }">
          {{ fmtPct(item.share) }}
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.io-matrix :deep(table) {
  white-space: nowrap;
}
</style>
