<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  fmtMetricRate,
  fmtMetricTotal,
  hitRatio,
  sumValues,
  type MatrixRow,
  type MetricMode,
} from './types'
import { fmtPct } from '@/utils/format'

const props = defineProps<{
  rows: MatrixRow[]
  windowSeconds: number
  metricMode: MetricMode
}>()

const { t } = useI18n()

const COUNT_METRICS = ['reads', 'hits', 'writes', 'extends']
const TIME_METRICS = ['read_time', 'write_time', 'extend_time']

const vacuumRows = computed(() => props.rows.filter((r) => r.context === 'vacuum'))

const metrics = computed(() => {
  const names = props.metricMode === 'count' ? COUNT_METRICS : TIME_METRICS

  return names.map((name) => {
    const value = sumValues(vacuumRows.value, name)
    return {
      name,
      value,
      total: fmtMetricTotal(value, props.metricMode),
      rate: fmtMetricRate(value, props.windowSeconds, props.metricMode),
    }
  })
})

const ratio = computed(() => hitRatio(vacuumRows.value))
const idle = computed(() => metrics.value.every((m) => m.value === 0))
</script>

<template>
  <v-card class="h-100">
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-broom" class="flex-shrink-0" />
      <span class="text-wrap">{{ t('io.vacuum.title') }}</span>
      <v-tooltip :text="t('io.vacuum.hint')" location="bottom" max-width="360">
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
      <v-alert v-if="idle" type="info" variant="tonal" density="compact">
        {{ t('io.vacuum.idle') }}
      </v-alert>

      <template v-else>
        <div v-if="ratio !== null" class="mb-3">
          <div class="text-caption text-medium-emphasis">{{ t('io.vacuum.hitRatio') }}</div>
          <div class="text-h6">{{ fmtPct(ratio * 100, 2) }}</div>
        </div>

        <v-table density="compact">
          <tbody>
            <tr v-for="m in metrics" :key="m.name">
              <td class="text-medium-emphasis">{{ m.name }}</td>
              <td class="text-right">{{ m.total }}</td>
              <td class="text-right text-medium-emphasis">{{ m.rate }}</td>
            </tr>
          </tbody>
        </v-table>
      </template>
    </v-card-text>
  </v-card>
</template>
