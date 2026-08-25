<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  fmtMetricRate,
  fmtMetricTotal,
  sumValues,
  type MatrixRow,
  type MetricMode,
} from './types'

const props = defineProps<{
  rows: MatrixRow[]
  windowSeconds: number
  metricMode: MetricMode
}>()

const { t } = useI18n()

const BULK_CONTEXTS = ['bulkread', 'bulkwrite']
const COUNT_METRICS = ['reads', 'writes', 'reuses', 'evictions']
const TIME_METRICS = ['read_time', 'write_time']

const sections = computed(() =>
  BULK_CONTEXTS.map((context) => {
    const rows = props.rows.filter((r) => r.context === context)
    const names = props.metricMode === 'count' ? COUNT_METRICS : TIME_METRICS

    const metrics = names.map((name) => {
      const value = sumValues(rows, name)
      return {
        name,
        value,
        total: fmtMetricTotal(value, props.metricMode),
        rate: fmtMetricRate(value, props.windowSeconds, props.metricMode),
      }
    })

    return { context, metrics, idle: metrics.every((m) => m.value === 0) }
  }),
)

const allIdle = computed(() => sections.value.every((s) => s.idle))
</script>

<template>
  <v-card class="h-100">
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-tray-arrow-down" class="flex-shrink-0" />
      <span class="text-wrap">{{ t('io.bulk.title') }}</span>
      <v-tooltip :text="t('io.bulk.hint')" location="bottom" max-width="360">
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
      <v-alert v-if="allIdle" type="info" variant="tonal" density="compact">
        {{ t('io.bulk.idle') }}
      </v-alert>

      <template v-else>
        <div v-for="s in sections" :key="s.context" class="mb-3">
          <div class="text-caption text-medium-emphasis mb-1">{{ s.context }}</div>
          <v-table v-if="!s.idle" density="compact">
            <tbody>
              <tr v-for="m in s.metrics" :key="m.name">
                <td class="text-medium-emphasis">{{ m.name }}</td>
                <td class="text-right">{{ m.total }}</td>
                <td class="text-right text-medium-emphasis">{{ m.rate }}</td>
              </tr>
            </tbody>
          </v-table>
          <div v-else class="text-caption text-disabled">{{ t('io.bulk.contextIdle') }}</div>
        </div>
      </template>
    </v-card-text>
  </v-card>
</template>
