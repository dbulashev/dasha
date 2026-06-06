<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { QueryReportMetrics } from '@/api/models/index'
import type { CompareSortKey } from './compare-types'
import { compareSortFieldMap } from './compare-types'
import { fmtBytes, fmtMs as fmtMsUtil, fmtPct, fmtInt } from '@/utils/format'

const props = defineProps<{
  metrics: QueryReportMetrics
  sortBy: CompareSortKey
}>()

const { t } = useI18n()

function isHighlighted(fieldKey: CompareSortKey): boolean {
  return props.sortBy === fieldKey
}

function isHighContrib(sortKey: CompareSortKey): boolean {
  const pctField = compareSortFieldMap[sortKey].pct
  const pct = props.metrics[pctField] as number | null | undefined
  return pct != null && pct > 5
}

function fmtMs(ms: number | null | undefined): string {
  return fmtMsUtil(ms, t)
}
</script>

<template>
  <v-row dense>
    <v-col cols="6" :class="{ 'report-highlight': isHighlighted('rows'), 'report-high-contrib': isHighContrib('rows') }">
      <div class="text-caption text-medium-emphasis">{{ t('header.rows') }}</div>
      <div class="text-body-2">{{ fmtInt(metrics.Rows) }} ({{ fmtPct(metrics.RowsPct) }})</div>
    </v-col>
    <v-col cols="6" :class="{ 'report-highlight': isHighlighted('calls'), 'report-high-contrib': isHighContrib('calls') }">
      <div class="text-caption text-medium-emphasis">{{ t('header.calls') }}</div>
      <div class="text-body-2">{{ fmtInt(metrics.Calls) }} ({{ fmtPct(metrics.CallsPct) }})</div>
    </v-col>
    <v-col cols="6" :class="{ 'report-highlight': isHighlighted('total_time'), 'report-high-contrib': isHighContrib('total_time') }">
      <div class="text-caption text-medium-emphasis">{{ t('header.totalTime') }}</div>
      <div class="text-body-2">{{ fmtMs(metrics.TotalTimeMs) }} ({{ fmtPct(metrics.TotalTimePct) }})</div>
    </v-col>
    <v-col cols="6">
      <div class="text-caption text-medium-emphasis">{{ t('header.cacheHitRatio') }}</div>
      <div class="text-body-2">{{ fmtPct(metrics.CacheHitRatio) }}</div>
    </v-col>
    <v-col cols="6">
      <div class="text-caption text-medium-emphasis">{{ t('header.execTime') }}</div>
      <div class="text-body-2">{{ fmtMs(metrics.ExecTimeMs) }}</div>
      <div class="text-caption text-medium-emphasis">
        {{ fmtMs(metrics.MinExecTimeMs) }} .. {{ fmtMs(metrics.MaxExecTimeMs) }}, {{ t('report.avg') }} {{ fmtMs(metrics.MeanExecTimeMs) }}<span v-if="metrics.StddevExecTimeMs != null">, {{ t('report.stddev') }}={{ fmtMs(metrics.StddevExecTimeMs) }}</span>
      </div>
    </v-col>
    <v-col cols="6">
      <div class="text-caption text-medium-emphasis">{{ t('header.planTime') }}</div>
      <div class="text-body-2">{{ fmtMs(metrics.PlanTimeMs) }}</div>
      <div class="text-caption text-medium-emphasis">
        {{ fmtMs(metrics.MinPlanTimeMs) }} .. {{ fmtMs(metrics.MaxPlanTimeMs) }}, {{ t('report.avg') }} {{ fmtMs(metrics.MeanPlanTimeMs) }}<span v-if="metrics.StddevPlanTimeMs != null">, {{ t('report.stddev') }}={{ fmtMs(metrics.StddevPlanTimeMs) }}</span>
      </div>
    </v-col>
    <v-col v-if="metrics.Usernames && metrics.Usernames.length" cols="12">
      <div class="d-flex flex-wrap align-center ga-1">
        <span class="text-caption text-medium-emphasis mr-1">{{ t('report.users', metrics.Usernames.length) }}:</span>
        <v-chip v-for="u in metrics.Usernames" :key="u" size="x-small" variant="tonal" label>{{ u }}</v-chip>
      </div>
    </v-col>
    <v-col cols="6" :class="{ 'report-highlight': isHighlighted('io_time'), 'report-high-contrib': isHighContrib('io_time') }">
      <div class="text-caption text-medium-emphasis">{{ t('header.ioTime') }}</div>
      <div class="text-body-2">{{ fmtMs(metrics.IoTimeMs) }} ({{ fmtPct(metrics.IoTimePct) }})</div>
    </v-col>
    <v-col cols="6" :class="{ 'report-highlight': isHighlighted('cpu_time'), 'report-high-contrib': isHighContrib('cpu_time') }">
      <div class="text-caption text-medium-emphasis">{{ t('header.cpuTime') }}</div>
      <div class="text-body-2">{{ fmtMs(metrics.CpuTimeMs) }} ({{ fmtPct(metrics.CpuTimePct) }})</div>
    </v-col>
    <v-col v-if="metrics.SharedBlksDirtiedPct != null || metrics.SharedBlksWrittenPct != null" cols="6">
      <div class="text-caption text-medium-emphasis">{{ t('header.sharedBlks') }}</div>
      <div class="text-body-2">dirtied: {{ fmtPct(metrics.SharedBlksDirtiedPct) }}, written: {{ fmtPct(metrics.SharedBlksWrittenPct) }}</div>
    </v-col>
    <v-col v-if="metrics.TempBlks != null" cols="6" :class="{ 'report-highlight': isHighlighted('temp_blks'), 'report-high-contrib': isHighContrib('temp_blks') }">
      <div class="text-caption text-medium-emphasis">{{ t('header.tmpBlks') }}</div>
      <div class="text-body-2">{{ fmtInt(metrics.TempBlks) }} ({{ fmtPct(metrics.TempBlksPct) }})</div>
    </v-col>
    <v-col v-if="metrics.WalBytes != null" cols="12" :class="{ 'report-highlight': isHighlighted('wal'), 'report-high-contrib': isHighContrib('wal') }">
      <div class="text-caption text-medium-emphasis">{{ t('header.wal') }}</div>
      <div class="text-body-2">{{ fmtBytes(metrics.WalBytes) }} ({{ fmtPct(metrics.WalBytesPct) }}, rec: {{ fmtInt(metrics.WalRecords) }}, fpi: {{ fmtInt(metrics.WalFpi) }})</div>
    </v-col>
  </v-row>
</template>

<style scoped>
.report-highlight {
  border-left: 3px solid rgb(var(--v-theme-primary));
  padding-left: 9px;
}

.report-high-contrib {
  background-color: rgba(var(--v-theme-warning), 0.08);
  border-radius: 4px;
  outline: 1px solid rgba(var(--v-theme-warning), 0.25);
  outline-offset: -2px;
}

.v-theme--dark .report-high-contrib {
  background-color: rgba(var(--v-theme-warning), 0.15);
}
</style>
