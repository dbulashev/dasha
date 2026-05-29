import type { ComposerTranslation } from 'vue-i18n'

// Rules whose recommendation card should fetch a small typed dataset from a
// dedicated /api/common/health-score/details/* endpoint and render it inline,
// instead of (or in addition to) the related-page link. Mapping intentionally
// kept on the frontend — the per-rule decision is a UI choice, not a backend
// contract.
export const RULES_WITH_INLINE_DETAILS = new Set<string>([
  'xid_wraparound_risk',
  'tables_with_autovacuum_off',
  'analyze_disabled_tables',
  'low_hot_update_ratio',
  'high_max_dead_ratio',
  'horizon_lag_xids',
])

export interface InlineColumn {
  key: string
  title: string
  format?: (v: unknown) => string
  cellClass?: string
}

export interface InlineSpec {
  needsDatabase: boolean
  columns: (t: ComposerTranslation) => InlineColumn[]
}

const fmtBigInt = (v: unknown): string =>
  typeof v === 'number' || typeof v === 'bigint' ? Number(v).toLocaleString() : String(v ?? '')

const fmtRatioPct = (v: unknown): string =>
  typeof v === 'number' && Number.isFinite(v) ? (v * 100).toFixed(1) + '%' : '—'

const fmtSeconds = (v: unknown): string =>
  typeof v === 'number' && Number.isFinite(v) ? v.toFixed(1) + ' с' : '—'

const fmtXidAge = (v: unknown): string => {
  if (typeof v !== 'number' && typeof v !== 'bigint') return String(v ?? '')
  const n = Number(v)
  if (n >= 1_000_000_000) return (n / 1_000_000_000).toFixed(2) + ' B'
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + ' M'
  return n.toLocaleString()
}

export const INLINE_SPECS: Record<string, InlineSpec> = {
  xid_wraparound_risk: {
    needsDatabase: false,
    columns: (t) => [
      { key: 'Database', title: t('healthScore.inline.col.database') },
      { key: 'XidAge', title: t('healthScore.inline.col.xidAge'), format: fmtXidAge },
    ],
  },
  tables_with_autovacuum_off: {
    needsDatabase: true,
    columns: (t) => [
      { key: 'Schema', title: t('healthScore.inline.col.schema') },
      { key: 'Table', title: t('healthScore.inline.col.table') },
      { key: 'RelOptions', title: t('healthScore.inline.col.reloptions') },
    ],
  },
  analyze_disabled_tables: {
    needsDatabase: true,
    columns: (t) => [
      { key: 'Schema', title: t('healthScore.inline.col.schema') },
      { key: 'Table', title: t('healthScore.inline.col.table') },
      { key: 'RelOptions', title: t('healthScore.inline.col.reloptions') },
    ],
  },
  low_hot_update_ratio: {
    needsDatabase: true,
    columns: (t) => [
      { key: 'Schema', title: t('healthScore.inline.col.schema') },
      { key: 'Table', title: t('healthScore.inline.col.table') },
      { key: 'Updates', title: t('healthScore.inline.col.updates'), format: fmtBigInt },
      { key: 'HotUpdates', title: t('healthScore.inline.col.hotUpdates'), format: fmtBigInt },
      { key: 'HotRatio', title: t('healthScore.inline.col.hotRatio'), format: fmtRatioPct },
    ],
  },
  high_max_dead_ratio: {
    needsDatabase: true,
    columns: (t) => [
      { key: 'Schema', title: t('healthScore.inline.col.schema') },
      { key: 'Table', title: t('healthScore.inline.col.table') },
      { key: 'LiveTuples', title: t('healthScore.inline.col.liveTuples'), format: fmtBigInt },
      { key: 'DeadTuples', title: t('healthScore.inline.col.deadTuples'), format: fmtBigInt },
      { key: 'DeadRatio', title: t('healthScore.inline.col.deadRatio'), format: fmtRatioPct },
    ],
  },
  horizon_lag_xids: {
    needsDatabase: false,
    columns: (t) => [
      { key: 'PID', title: 'PID' },
      { key: 'Username', title: t('healthScore.inline.col.username') },
      { key: 'State', title: t('healthScore.inline.col.state') },
      {
        key: 'XactDurationSeconds',
        title: t('healthScore.inline.col.xactDuration'),
        format: fmtSeconds,
      },
      { key: 'BackendXmin', title: 'backend_xmin' },
      {
        key: 'Query',
        title: t('healthScore.inline.col.query'),
        cellClass: 'inline-query-cell',
      },
    ],
  },
}
