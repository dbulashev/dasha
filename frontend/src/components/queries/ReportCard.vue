<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { QueryReport } from '@/api/models/index'
import { fmtBytes, fmtMs as fmtMsUtil, fmtPct, fmtInt } from '@/utils/format'
import { highlightSql, copyToClipboard } from '@/utils/sql'
import '@/assets/sql-highlight.css'

type ReportSortKey = 'total_time' | 'calls' | 'wal' | 'rows' | 'cpu_time' | 'io_time' | 'temp_blks'

const props = defineProps<{
  item: QueryReport
  sortBy: ReportSortKey
}>()

const emit = defineEmits<{
  showSql: [item: QueryReport]
}>()

const { t } = useI18n()

const sortFieldMap: Record<ReportSortKey, { value: keyof QueryReport; pct: keyof QueryReport }> = {
  total_time: { value: 'TotalTimeMs', pct: 'TotalTimePct' },
  calls: { value: 'Calls', pct: 'CallsPct' },
  wal: { value: 'WalBytes', pct: 'WalBytesPct' },
  rows: { value: 'Rows', pct: 'RowsPct' },
  cpu_time: { value: 'CpuTimeMs', pct: 'CpuTimePct' },
  io_time: { value: 'IoTimeMs', pct: 'IoTimePct' },
  temp_blks: { value: 'TempBlks', pct: 'TempBlksPct' },
}

function isHighlightedField(fieldKey: ReportSortKey): boolean {
  return props.sortBy === fieldKey
}

function isHighContribution(sortKey: ReportSortKey): boolean {
  const pctField = sortFieldMap[sortKey].pct
  const pct = props.item[pctField] as number | null | undefined
  return pct != null && pct > 5
}

function fmtMs(ms: number | null | undefined): string {
  return fmtMsUtil(ms, t)
}

function truncateSql(sql: string, maxLen = 120): string {
  if (sql.length <= maxLen) return sql
  return sql.substring(0, maxLen) + '…'
}

</script>

<template>
  <v-card variant="outlined" class="mb-3">
    <v-card-title class="text-body-1 pb-1 d-flex align-center">
      <span>queryid: <span class="text-mono">{{ item.QueryID }}</span></span>
      <v-btn icon="mdi-content-copy" variant="text" size="x-small" class="ml-1" @click="copyToClipboard(String(item.QueryID))" />
    </v-card-title>
    <v-card-text class="pt-0">
      <v-row dense>
        <v-col cols="6" md="3" :class="{ 'report-highlight': isHighlightedField('rows'), 'report-high-contrib': isHighContribution('rows') }">
          <div class="text-caption text-medium-emphasis">{{ t('header.rows') }}</div>
          <div class="text-body-2">{{ fmtInt(item.Rows) }} ({{ fmtPct(item.RowsPct) }})</div>
        </v-col>
        <v-col cols="6" md="3" :class="{ 'report-highlight': isHighlightedField('calls'), 'report-high-contrib': isHighContribution('calls') }">
          <div class="text-caption text-medium-emphasis">{{ t('header.calls') }}</div>
          <div class="text-body-2">{{ fmtInt(item.Calls) }} ({{ fmtPct(item.CallsPct) }})</div>
        </v-col>
        <v-col cols="6" md="3" :class="{ 'report-highlight': isHighlightedField('total_time'), 'report-high-contrib': isHighContribution('total_time') }">
          <div class="text-caption text-medium-emphasis">{{ t('header.totalTime') }}</div>
          <div class="text-body-2">{{ fmtMs(item.TotalTimeMs) }} ({{ fmtPct(item.TotalTimePct) }})</div>
        </v-col>
        <v-col cols="6" md="3">
          <div class="text-caption text-medium-emphasis">{{ t('header.cacheHitRatio') }}</div>
          <div class="text-body-2">{{ fmtPct(item.CacheHitRatio) }}</div>
        </v-col>
        <v-col cols="6" md="3">
          <div class="text-caption text-medium-emphasis">{{ t('header.execTime') }}</div>
          <div class="text-body-2">{{ fmtMs(item.ExecTimeMs) }}</div>
          <div class="text-caption text-medium-emphasis">{{ fmtMs(item.MinExecTimeMs) }} .. {{ fmtMs(item.MaxExecTimeMs) }}, {{ t('report.avg') }} {{ fmtMs(item.MeanExecTimeMs) }}</div>
        </v-col>
        <v-col cols="6" md="3">
          <div class="text-caption text-medium-emphasis">{{ t('header.planTime') }}</div>
          <div class="text-body-2">{{ fmtMs(item.PlanTimeMs) }}</div>
          <div class="text-caption text-medium-emphasis">{{ fmtMs(item.MinPlanTimeMs) }} .. {{ fmtMs(item.MaxPlanTimeMs) }}, {{ t('report.avg') }} {{ fmtMs(item.MeanPlanTimeMs) }}</div>
        </v-col>
        <v-col cols="6" md="3" :class="{ 'report-highlight': isHighlightedField('io_time'), 'report-high-contrib': isHighContribution('io_time') }">
          <div class="text-caption text-medium-emphasis">{{ t('header.ioTime') }}</div>
          <div class="text-body-2">{{ fmtMs(item.IoTimeMs) }} ({{ fmtPct(item.IoTimePct) }})</div>
        </v-col>
        <v-col cols="6" md="3" :class="{ 'report-highlight': isHighlightedField('cpu_time'), 'report-high-contrib': isHighContribution('cpu_time') }">
          <div class="text-caption text-medium-emphasis">{{ t('header.cpuTime') }}</div>
          <div v-if="item.CpuTimeMs == null" class="text-body-2">
            <v-tooltip :text="t('report.cpuTimeUnavailable')" location="bottom" max-width="380">
              <template #activator="{ props }">
                <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div v-else class="text-body-2">{{ fmtMs(item.CpuTimeMs) }} ({{ fmtPct(item.CpuTimePct) }})</div>
        </v-col>
        <v-col v-if="item.SharedBlksDirtiedPct != null || item.SharedBlksWrittenPct != null" cols="6" md="4">
          <div class="text-caption text-medium-emphasis">{{ t('header.sharedBlks') }}</div>
          <div class="text-body-2">dirtied: {{ fmtPct(item.SharedBlksDirtiedPct) }}, written: {{ fmtPct(item.SharedBlksWrittenPct) }}</div>
        </v-col>
        <v-col v-if="item.TempBlks != null" cols="6" md="4" :class="{ 'report-highlight': isHighlightedField('temp_blks'), 'report-high-contrib': isHighContribution('temp_blks') }">
          <div class="text-caption text-medium-emphasis">{{ t('header.tmpBlks') }}</div>
          <div class="text-body-2">{{ fmtInt(item.TempBlks) }} ({{ fmtPct(item.TempBlksPct) }})</div>
        </v-col>
        <v-col v-if="item.WalBytes != null" cols="6" md="4" :class="{ 'report-highlight': isHighlightedField('wal'), 'report-high-contrib': isHighContribution('wal') }">
          <div class="text-caption text-medium-emphasis">{{ t('header.wal') }}</div>
          <div class="text-body-2">{{ fmtBytes(item.WalBytes) }} ({{ fmtPct(item.WalBytesPct) }}, rec: {{ fmtInt(item.WalRecords) }}, fpi: {{ fmtInt(item.WalFpi) }})</div>
        </v-col>
      </v-row>
      <div class="mt-2 d-flex align-center">
        <code class="sql-highlight text-mono text-body-2 text-medium-emphasis flex-grow-1 sql-truncate" v-html="highlightSql(truncateSql(item.Query))"></code>
        <v-btn icon="mdi-content-copy" variant="text" size="x-small" class="ml-1 flex-shrink-0" @click="copyToClipboard(item.Query)" />
        <v-btn v-if="item.Query.length > 120" size="small" variant="text" class="ml-1 flex-shrink-0" @click="emit('showSql', item)">
          {{ t('report.showSql') }}
        </v-btn>
      </div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.report-highlight {
  border-left: 3px solid rgb(var(--v-theme-primary));
  padding-left: 9px;
}

.sql-truncate {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
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
