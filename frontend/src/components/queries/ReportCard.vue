<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import hljs from 'highlight.js/lib/core'
import pgsql from 'highlight.js/lib/languages/pgsql'
import type { QueryReport } from '@/api/models/index'
import { fmtBytes } from '@/utils/format'

hljs.registerLanguage('pgsql', pgsql)

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

function highlightSql(sql: string): string {
  return hljs.highlight(sql, { language: 'pgsql' }).value
}

function truncateSql(sql: string, maxLen = 120): string {
  if (sql.length <= maxLen) return sql
  return sql.substring(0, maxLen) + '…'
}

function copyToClipboard(text: string) {
  if (navigator.clipboard) {
    navigator.clipboard.writeText(text)
  } else {
    const ta = document.createElement('textarea')
    ta.value = text
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
}

function fmtMs(ms: number | null | undefined): string {
  if (ms == null) return '—'
  if (Math.abs(ms) >= 3600000) {
    const h = Math.floor(Math.abs(ms) / 3600000)
    const m = Math.floor((Math.abs(ms) % 3600000) / 60000)
    const sign = ms < 0 ? '-' : ''
    return m > 0 ? `${sign}${h} ${t('time.h')} ${m} ${t('time.min')}` : `${sign}${h} ${t('time.h')}`
  }
  if (Math.abs(ms) >= 60000) {
    const m = Math.floor(Math.abs(ms) / 60000)
    const s = Math.floor((Math.abs(ms) % 60000) / 1000)
    const sign = ms < 0 ? '-' : ''
    return s > 0 ? `${sign}${m} ${t('time.min')} ${s} ${t('time.sec')}` : `${sign}${m} ${t('time.min')}`
  }
  if (Math.abs(ms) >= 1000) return `${(ms / 1000).toFixed(1)} ${t('time.sec')}`
  if (Math.abs(ms) >= 1) return `${ms.toFixed(1)} ${t('time.ms')}`
  if (Math.abs(ms) >= 0.001) return `${(ms * 1000).toFixed(1)} ${t('time.us')}`
  return `0 ${t('time.ms')}`
}

function fmtPct(v: number | null | undefined): string {
  if (v == null) return '—'
  return v.toFixed(1) + '%'
}

function fmtInt(v: number | null | undefined): string {
  if (v == null) return '—'
  return v.toLocaleString()
}

</script>

<template>
  <v-card variant="outlined" class="mb-3">
    <v-card-title class="text-body-1 pb-1 d-flex align-center">
      <span>queryid: <span style="font-family: monospace;">{{ item.QueryID }}</span></span>
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
          <div class="text-body-2">{{ fmtMs(item.CpuTimeMs) }} ({{ fmtPct(item.CpuTimePct) }})</div>
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
        <code class="sql-highlight text-body-2 text-medium-emphasis flex-grow-1" style="white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-family: monospace;" v-html="highlightSql(truncateSql(item.Query))"></code>
        <v-btn icon="mdi-content-copy" variant="text" size="x-small" class="ml-1 flex-shrink-0" @click="copyToClipboard(item.Query)" />
        <v-btn v-if="item.Query.length > 120" size="small" variant="text" class="ml-1 flex-shrink-0" @click="emit('showSql', item)">
          {{ t('report.showSql') }}
        </v-btn>
      </div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.sql-highlight :deep(.hljs-keyword) { color: #cf222e; }
.sql-highlight :deep(.hljs-string) { color: #0a3069; }
.sql-highlight :deep(.hljs-number) { color: #0550ae; }
.sql-highlight :deep(.hljs-built_in) { color: #8250df; }
.sql-highlight :deep(.hljs-type) { color: #8250df; }
.sql-highlight :deep(.hljs-comment) { color: #6e7781; }
.sql-highlight :deep(.hljs-operator) { color: #cf222e; }

.v-theme--dark .sql-highlight :deep(.hljs-keyword) { color: #ff7b72; }
.v-theme--dark .sql-highlight :deep(.hljs-string) { color: #a5d6ff; }
.v-theme--dark .sql-highlight :deep(.hljs-number) { color: #79c0ff; }
.v-theme--dark .sql-highlight :deep(.hljs-built_in) { color: #d2a8ff; }
.v-theme--dark .sql-highlight :deep(.hljs-type) { color: #d2a8ff; }
.v-theme--dark .sql-highlight :deep(.hljs-comment) { color: #8b949e; }
.v-theme--dark .sql-highlight :deep(.hljs-operator) { color: #ff7b72; }

.report-highlight {
  border-left: 3px solid rgb(var(--v-theme-primary));
  padding-left: 9px !important;
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
