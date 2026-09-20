import type { LogPlanConfiguration, LogPlansSummary } from '@/api/models'
import { fmtCompact } from '@/utils/format'

type TFunc = (key: string, named?: Record<string, unknown>) => string

const PARSED_FORMATS = ['text', 'json']

// What keeps plans out of the log. Each state names what to switch on, rather
// than one "nothing found".
export function planBlocker(c: LogPlanConfiguration | undefined, t: TFunc): string {
  if (!c) return ''
  if (!c.auto_explain) return t('logs.insights.setup.noExtension', { instance: c.instance })

  if (c.log_min_duration_ms != null && c.log_min_duration_ms < 0) {
    return t('logs.insights.setup.loggingOff', { instance: c.instance })
  }

  if (c.log_format && !PARSED_FORMATS.includes(c.log_format)) {
    return t('logs.insights.setup.format', { format: c.log_format })
  }

  return ''
}

export function planEmptyReason(
  summary: LogPlansSummary | null,
  t: TFunc,
  te: (key: string) => boolean,
): string {
  const code = summary?.empty_reason
  if (!code) return ''

  const key = `logs.insights.emptyReason.${code}`
  return te(key) ? t(key) : code
}

// What the plans of this window do and do not carry. Facts about the scan, next
// to the rest of its numbers.
export function planNotes(
  c: LogPlanConfiguration | undefined,
  summary: LogPlansSummary | null,
  t: TFunc,
): string[] {
  const out: string[] = []
  if (c?.auto_explain && c.log_analyze === false) out.push(t('logs.insights.setup.noAnalyze'))
  if (c?.compute_query_id === 'off') out.push(t('logs.insights.setup.noQueryId'))

  const without = summary?.without_query_id ?? 0
  if (without > 0 && c?.compute_query_id !== 'off') {
    out.push(t('logs.insights.setup.someWithoutQueryId', { n: fmtCompact(without) }))
  }

  const threshold = c?.log_min_duration_ms
  if (c?.auto_explain && threshold != null && threshold > 0) {
    out.push(t('logs.insights.setup.threshold', { ms: fmtCompact(threshold) }))
  }

  return out
}
