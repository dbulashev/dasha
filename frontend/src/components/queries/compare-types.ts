import type { QueryReportMetrics } from '@/api/models/index'

export type CompareSortKey = 'total_time' | 'calls' | 'wal' | 'rows' | 'cpu_time' | 'io_time' | 'temp_blks'

export interface SortFieldDef {
  value: keyof QueryReportMetrics
  pct: keyof QueryReportMetrics
}

export const compareSortFieldMap: Record<CompareSortKey, SortFieldDef> = {
  total_time: { value: 'TotalTimeMs', pct: 'TotalTimePct' },
  calls: { value: 'Calls', pct: 'CallsPct' },
  wal: { value: 'WalBytes', pct: 'WalBytesPct' },
  rows: { value: 'Rows', pct: 'RowsPct' },
  cpu_time: { value: 'CpuTimeMs', pct: 'CpuTimePct' },
  io_time: { value: 'IoTimeMs', pct: 'IoTimePct' },
  temp_blks: { value: 'TempBlks', pct: 'TempBlksPct' },
}
