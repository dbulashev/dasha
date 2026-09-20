<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { getLogsScanGroup, getLogsScanGroups } from '@/api/gen/default/default'
import type {
  GetLogsScanGroupsOrder,
  LogPlanGroup,
  LogPlanGroupPage,
  LogPlansSummary,
  PlanDurationStats,
  PlanFinding,
  PlanSummary,
} from '@/api/models'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { usePrefsStore } from '@/stores/prefs'
import { ApiError, assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtCompact, fmtDateTime, fmtMs } from '@/utils/format'
import { copyToClipboard, highlightSql, truncateSql } from '@/utils/sql'
import { severityColor, severityRank } from '@/components/explain/planText'
import PlanTree from '@/components/explain/PlanTree.vue'
import PaginationControls from '@/components/PaginationControls.vue'
import SqlDialog from '@/components/queries/SqlDialog.vue'
import PlanFindingsList from './PlanFindingsList.vue'
import '@/assets/sql-highlight.css'

interface GroupRow {
  ord: number
  queryId?: string
  hash: string
  count: number
  durations: PlanDurationStats
  firstSeen: string
  lastSeen: string
  queryText: string
  omittedBytes?: number
  findings: PlanFinding[]
  plan?: PlanSummary
}

const props = defineProps<{
  scanId?: string
  groups: LogPlanGroup[]
  summary: LogPlansSummary | null
  queryId?: string
  emptyReason?: string
}>()

const emit = defineEmits<{
  scanGone: []
}>()

const { t, te } = useI18n()
const route = useRoute()
const { clusterName } = useClusterInfo()
const prefs = usePrefsStore()

const order = ref<GetLogsScanGroupsOrder>('sum')
const onlyFindings = ref(false)
const page = ref(1)
const pagedRows = ref<GroupRow[]>([])
const total = ref(0)
const loading = ref(false)
const errorMsg = ref('')

// The snapshot is the authority while it exists; the groups of the answer are
// its top and only stand in when nothing was stored.
const inlineRows = computed<GroupRow[]>(() =>
  props.groups
    .filter(g => !onlyFindings.value || (g.plan?.findings?.length ?? 0) > 0)
    .map(g => ({
      ord: g.ord,
      queryId: g.query_id,
      hash: g.hash,
      count: g.count,
      durations: g.durations,
      firstSeen: g.first_seen,
      lastSeen: g.last_seen,
      queryText: g.plan?.query_text ?? '',
      omittedBytes: g.plan?.query_text_omitted_bytes,
      findings: g.plan?.findings ?? [],
      plan: g.plan,
    })),
)

const rows = computed(() => (props.scanId ? pagedRows.value : inlineRows.value))

const hasMore = computed(() =>
  props.scanId ? total.value > page.value * prefs.pageSize : false,
)

async function loadPage(p = 1) {
  if (!props.scanId) return
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await getLogsScanGroups(props.scanId, {
      query_id: props.queryId || undefined,
      with_findings: onlyFindings.value || undefined,
      order: order.value,
      limit: prefs.pageSize,
      offset: (p - 1) * prefs.pageSize,
    })
    const body = assertOk<LogPlanGroupPage>(res)
    pagedRows.value = (body.items ?? []).map(r => ({
      ord: r.ord,
      queryId: r.query_id,
      hash: r.hash,
      count: r.count,
      durations: r.durations,
      firstSeen: r.first_seen,
      lastSeen: r.last_seen,
      queryText: r.query_text,
      omittedBytes: r.query_text_omitted_bytes,
      findings: r.findings ?? [],
    }))
    total.value = body.total
    page.value = p
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      emit('scanGone')
    } else {
      errorMsg.value = getErrorMessage(err)
    }
    pagedRows.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

const expandedOrd = ref<number | null>(null)
const expandedPlan = ref<PlanSummary | null>(null)
const planLoading = ref(false)
const highlight = ref<number[] | null>(null)

watch(
  [() => props.scanId, () => props.queryId, order, onlyFindings, () => prefs.pageSize],
  () => {
    expandedOrd.value = null
    if (props.scanId) loadPage(1)
  },
  { immediate: true },
)

// A row opened while another is still loading: only the last request may write
// the panel.
let planRequest = 0

async function toggleRow(row: GroupRow) {
  const req = ++planRequest

  if (expandedOrd.value === row.ord) {
    expandedOrd.value = null
    planLoading.value = false
    return
  }

  expandedOrd.value = row.ord
  highlight.value = null
  expandedPlan.value = row.plan ?? null
  if (row.plan || !props.scanId) {
    planLoading.value = false
    return
  }

  planLoading.value = true
  try {
    const res = await getLogsScanGroup(props.scanId, row.ord)
    if (req !== planRequest) return
    expandedPlan.value = assertOk<LogPlanGroup>(res).plan
  } catch (err) {
    if (req !== planRequest) return
    if (err instanceof ApiError && err.status === 404) emit('scanGone')
    else errorMsg.value = getErrorMessage(err)
    expandedOrd.value = null
  } finally {
    if (req === planRequest) planLoading.value = false
  }
}

function severityCounts(findings: PlanFinding[]): { severity: string; count: number }[] {
  const bySeverity = new Map<string, number>()
  for (const f of findings) bySeverity.set(f.severity, (bySeverity.get(f.severity) ?? 0) + 1)
  return [...bySeverity.entries()]
    .map(([severity, count]) => ({ severity, count }))
    .sort((a, b) => severityRank(a.severity) - severityRank(b.severity))
}

const orderItems = computed(() => [
  { value: 'sum', title: t('logs.insights.order.sum') },
  { value: 'max', title: t('logs.insights.order.max') },
  { value: 'count', title: t('logs.insights.order.count') },
])

const notParsed = computed(() => props.summary?.not_parsed ?? [])

const dormantSummary = computed(() =>
  (props.summary?.dormant ?? []).map(d => {
    const missing = (d.missing ?? []).map(m => t(`plan.missing.${m}`)).join(', ')
    return t('logs.insights.dormantGroups', { code: d.code, missing, groups: d.groups ?? 0 })
  }),
)

function notParsedText(code: string, count: number): string {
  const key = `logs.insights.notParsed.${code}`
  return `${te(key) ? t(key) : code}: ${fmtCompact(count)}`
}

const totalGroups = computed(() => props.summary?.total_groups ?? rows.value.length)

const sqlDialog = ref(false)
const sqlText = ref('')
const sqlQueryId = ref('')

function showSql(row: GroupRow) {
  sqlText.value = row.plan?.query_text ?? row.queryText
  sqlQueryId.value = row.queryId ?? row.hash
  sqlDialog.value = true
}

// The log search around the last plan of the group: the same statement, a window
// wide enough to hold the records the plan came with.
const LOGS_MARGIN_MS = 5 * 60 * 1000

function logsLink(row: GroupRow) {
  const at = Date.parse(row.lastSeen)
  if (!Number.isFinite(at) || !clusterName.value) return null

  return {
    name: 'logs',
    params: { clustername: clusterName.value },
    query: {
      ...(route.query.host ? { host: String(route.query.host) } : {}),
      ...(route.query.db ? { db: String(route.query.db) } : {}),
      range: 'custom',
      from: new Date(at - LOGS_MARGIN_MS).toISOString(),
      to: new Date(at + LOGS_MARGIN_MS).toISOString(),
      ...(row.queryId ? { query_id: row.queryId } : {}),
    },
  }
}

function capabilityText(plan: PlanSummary): string {
  const missing: string[] = []
  if (!plan.capabilities.actual) missing.push(t('plan.missing.actual'))
  if (!plan.capabilities.timing) missing.push(t('plan.missing.timing'))
  if (!plan.capabilities.buffers) missing.push(t('plan.missing.buffers'))
  return missing.length ? t('plan.withoutData', { missing: missing.join(', ') }) : ''
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="text-subtitle-1 d-flex align-center flex-wrap ga-2">
      <span class="text-caption text-medium-emphasis">
        {{ t('logs.insights.plans.counts', {
          groups: fmtCompact(totalGroups),
          parsed: fmtCompact(props.summary?.parsed ?? 0),
          records: fmtCompact(props.summary?.records ?? 0),
        }) }}
      </span>
      <v-spacer />
      <v-switch
        v-model="onlyFindings"
        :label="t('logs.insights.plans.onlyFindings')"
        color="primary"
        density="compact"
        hide-details
        class="me-4 flex-grow-0"
      />
      <v-select
        v-if="props.scanId"
        v-model="order"
        :items="orderItems"
        :label="t('logs.insights.order.label')"
        density="compact"
        hide-details
        style="max-width: 260px"
      />
    </v-card-title>

    <v-card-text>
      <v-alert v-if="errorMsg" type="error" variant="tonal" class="mb-3" closable @click:close="errorMsg = ''">
        {{ errorMsg }}
      </v-alert>

      <div v-if="notParsed.length" class="text-caption text-medium-emphasis mb-2">
        {{ t('logs.insights.notParsedTitle') }}:
        <span v-for="n in notParsed" :key="n.code" class="me-3">{{ notParsedText(n.code, n.count) }}</span>
      </div>

      <div v-for="(line, i) in dormantSummary" :key="i" class="text-caption text-medium-emphasis mb-1">
        {{ line }}
      </div>

      <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-2" />

      <v-table density="compact">
        <thead>
          <tr>
            <th>{{ t('logs.insights.plans.query') }}</th>
            <th class="text-right plan-num">{{ t('logs.insights.plans.count') }}</th>
            <th class="text-right plan-num">{{ t('logs.insights.plans.sum') }}</th>
            <th class="text-right plan-num">p50</th>
            <th class="text-right plan-num">p95</th>
            <th class="text-right plan-num">max</th>
            <th>{{ t('logs.col.lastSeen') }}</th>
            <th>{{ t('logs.insights.plans.findings') }}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <template v-for="row in rows" :key="row.ord">
            <tr class="plan-group-row" @click="toggleRow(row)">
              <td class="plan-group-query">
                <code class="sql-highlight text-mono text-caption" v-html="highlightSql(truncateSql(row.queryText, 120))"></code>
              </td>
              <td class="text-right plan-num">{{ fmtCompact(row.count) }}</td>
              <td class="text-right plan-num">{{ fmtMs(row.durations.sum_ms, t) }}</td>
              <td class="text-right plan-num">{{ fmtMs(row.durations.p50_ms, t) }}</td>
              <td class="text-right plan-num">{{ fmtMs(row.durations.p95_ms, t) }}</td>
              <td class="text-right plan-num">{{ fmtMs(row.durations.max_ms, t) }}</td>
              <td>{{ fmtDateTime(row.lastSeen) }}</td>
              <td>
                <v-chip
                  v-for="s in severityCounts(row.findings)"
                  :key="s.severity"
                  :color="severityColor(s.severity)"
                  size="x-small"
                  variant="flat"
                  label
                  class="me-1"
                >
                  {{ s.count }}
                  <v-tooltip activator="parent" location="top">{{ s.severity }}</v-tooltip>
                </v-chip>
              </td>
              <td class="text-right">
                <v-icon
                  :icon="expandedOrd === row.ord ? 'mdi-chevron-up' : 'mdi-chevron-down'"
                  size="small"
                />
              </td>
            </tr>
            <tr v-if="expandedOrd === row.ord">
              <td colspan="9" class="pa-4 plan-group-detail">
                <v-progress-linear v-if="planLoading" indeterminate color="primary" />
                <template v-else-if="expandedPlan">
                  <div class="d-flex align-start flex-wrap ga-2 mb-2">
                    <PlanFindingsList
                      :findings="expandedPlan.findings"
                      :dormant="expandedPlan.dormant"
                      :selected="highlight"
                      class="flex-grow-1"
                      @select="highlight = $event"
                    />
                    <v-btn
                      v-if="logsLink(row)"
                      :to="logsLink(row) ?? undefined"
                      size="small"
                      variant="text"
                      prepend-icon="mdi-text-box-search-outline"
                      @click.stop
                    >
                      {{ t('logs.insights.plans.findInLogs') }}
                    </v-btn>
                    <v-btn size="small" variant="text" prepend-icon="mdi-code-tags" @click.stop="showSql(row)">
                      SQL
                    </v-btn>
                  </div>

                  <div class="text-caption text-medium-emphasis mb-2 d-flex align-center flex-wrap ga-1">
                    <template v-if="row.queryId">
                      <span>queryid: {{ row.queryId }}</span>
                      <v-btn
                        icon="mdi-content-copy"
                        variant="text"
                        size="x-small"
                        :title="t('logs.insights.plans.copyQueryId')"
                        @click.stop="copyToClipboard(row.queryId ?? '')"
                      />
                    </template>
                    <span class="me-3 text-mono">{{ t('logs.insights.plans.shape') }}: {{ row.hash }}</span>
                    <v-chip v-if="expandedPlan.generic" size="x-small" color="info" variant="tonal" label class="me-2">
                      {{ t('plan.generic') }}
                    </v-chip>
                    <span v-if="expandedPlan.planning_time_ms != null" class="me-3">
                      {{ t('plan.planningTime') }}: {{ fmtMs(expandedPlan.planning_time_ms, t) }}
                    </span>
                    <span v-if="expandedPlan.execution_time_ms != null" class="me-3">
                      {{ t('plan.executionTime') }}: {{ fmtMs(expandedPlan.execution_time_ms, t) }}
                    </span>
                    <span v-if="capabilityText(expandedPlan)">{{ capabilityText(expandedPlan) }}</span>
                  </div>

                  <PlanTree
                    :root="expandedPlan.root"
                    :findings="expandedPlan.findings"
                    :highlight="highlight"
                  />

                  <div v-if="expandedPlan.jit" class="text-caption text-medium-emphasis mt-2">
                    JIT: {{ expandedPlan.jit.functions }} fn · {{ fmtMs(expandedPlan.jit.total_ms, t) }}
                  </div>
                  <div
                    v-for="trg in expandedPlan.triggers ?? []"
                    :key="trg.name"
                    class="text-caption text-medium-emphasis"
                  >
                    {{ trg.name }}: {{ fmtMs(trg.time_ms, t) }} · {{ fmtCompact(trg.calls) }}
                  </div>
                </template>
              </td>
            </tr>
          </template>
          <tr v-if="!rows.length && !loading">
            <td colspan="9" class="text-center text-medium-emphasis py-4">
              {{ props.emptyReason || t('logs.insights.plans.empty') }}
            </td>
          </tr>
        </tbody>
      </v-table>

      <PaginationControls
        v-if="props.scanId"
        :page="page"
        :has-more="hasMore"
        @update:page="loadPage"
      />
    </v-card-text>

    <SqlDialog v-model="sqlDialog" :query-id="sqlQueryId" :sql="sqlText" />
  </v-card>
</template>

<style scoped>
.plan-group-row {
  cursor: pointer;
}

.plan-group-query {
  max-width: 400px;
  word-break: break-word;
}

.plan-num {
  white-space: nowrap;
}

.plan-group-detail {
  background: rgba(var(--v-theme-on-surface), 0.04);
}
</style>
