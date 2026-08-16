<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIndexesAdvisor } from '@/api/gen/default/default'
import type {
  IndexAdvisorCandidate,
  IndexAdvisorCoveredQuery,
  IndexAdvisorNotParsed,
  IndexAdvisorReport,
  IndexAdvisorSummary,
  IndexAdvisorWarning,
} from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useDescribeLink } from '@/composables/useDescribeLink'
import { useViewError } from '@/composables/useViewError'
import { useExcludeUsersStore } from '@/stores/excludeUsers'
import { usePrefsStore } from '@/stores/prefs'
import { ApiError, assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtCompact, fmtInt, fmtPct } from '@/utils/format'
import { copyToClipboard, highlightSql, truncateSql } from '@/utils/sql'
import PaginationControls from '@/components/PaginationControls.vue'
import SqlDialog from '@/components/queries/SqlDialog.vue'
import '@/assets/sql-highlight.css'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { describeLink } = useDescribeLink()
const { t, te } = useI18n()
const { onError } = useViewError()
const excludeUsersStore = useExcludeUsersStore()
const prefs = usePrefsStore()

const candidates = ref<IndexAdvisorCandidate[]>([])
const notParsed = ref<IndexAdvisorNotParsed[]>([])
const summary = ref<IndexAdvisorSummary | null>(null)
const durationMs = ref(0)
const loading = ref(false)
const page = ref(1)
const hasMore = ref(false)
// 404 also means the advisor is switched off; a disabled feature should be absent.
const unavailable = ref(false)

// The report is slow, so switching database leaves an older request in flight.
let reqId = 0

async function load(p = 1) {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  const myId = ++reqId
  loading.value = true
  try {
    const pageSize = prefs.pageSize
    // Same exclusion list the query report uses — same pg_stat_statements.
    const excluded = excludeUsersStore.getExcludeUsers(clusterName.value)
    const res = await getIndexesAdvisor({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      exclude_users: excluded.length ? excluded : undefined,
      limit: pageSize,
      offset: (p - 1) * pageSize,
    })
    const body = assertOk<IndexAdvisorReport>(res)
    if (myId !== reqId) return // superseded — leave state to the newer load
    unavailable.value = false
    candidates.value = body?.candidates ?? []
    notParsed.value = body?.not_parsed ?? []
    summary.value = body?.summary ?? null
    durationMs.value = body?.duration_ms ?? 0
    page.value = p
    // total is what the ranking saw, before the page was cut.
    hasMore.value = (body?.total ?? 0) > p * pageSize
  } catch (err) {
    if (myId !== reqId) return
    if (err instanceof ApiError && err.status === 404) {
      unavailable.value = true
    } else {
      onError(getErrorMessage(err), err)
    }
    candidates.value = []
    notParsed.value = []
    summary.value = null
  } finally {
    if (myId === reqId) loading.value = false
  }
}

watch(
  [clusterName, hostName, databaseName, () => prefs.pageSize],
  () => load(),
  { immediate: true },
)

// One table can carry several candidates, so the columns belong in the key.
function rowKey(item: IndexAdvisorCandidate) {
  return `${item.schema}.${item.table}(${item.columns.join(',')})`
}

const headers = computed(() => [
  { title: t('header.schema'), key: 'schema' },
  { title: t('header.table'), key: 'table' },
  { title: t('indexes.advisor.columns'), key: 'columns', sortable: false },
  { title: t('indexes.advisor.weight'), key: 'weight_pct' },
  { title: t('indexes.advisor.tableRows'), key: 'table_rows' },
  { title: t('indexes.advisor.warningsHeader'), key: 'warnings', sortable: false },
])

const WARNING_COLOR: Record<string, string> = {
  write_heavy: 'warning',
  low_weight: 'grey',
  overlaps_missing_signal: 'info',
  partition_root: 'info',
  stats_missing: 'warning',
  wide_index: 'warning',
  matview: 'info',
}

// Every code gets every parameter — vue-i18n drops the ones its phrasing omits.
// An unknown code falls back to the bare code so the row still renders.
function warningText(w: IndexAdvisorWarning): string {
  const key = `indexes.advisor.warnings.${w.code}`
  if (!te(key)) return w.code

  const p = w.params ?? {}
  return t(key, {
    writeCalls: fmtCompact(p.write_calls ?? 0),
    readCalls: fmtCompact(p.read_calls ?? 0),
    weight: fmtPct(p.weight_pct ?? 0, 2),
    columns: p.columns ?? 0,
    requested: p.requested ?? 0,
    rows: fmtCompact(p.rows ?? 0),
    idxScanPct: fmtPct(p.idx_scan_pct ?? 0),
  })
}

function warningLabel(w: IndexAdvisorWarning): string {
  const key = `indexes.advisor.warningLabels.${w.code}`
  return te(key) ? t(key) : w.code
}

function reasonText(n: IndexAdvisorNotParsed): string {
  const key = `indexes.advisor.notParsed.${n.reason_code}`
  return te(key) ? t(key) : n.reason_code
}

const notParsedTotal = computed(() => summary.value?.not_parsed_count ?? 0)

// Without pgss there is no workload, so "no candidates" would claim too much.
const pgssReadable = computed(() => summary.value == null || summary.value.pgss_available)

// How much of the workload was read keeps an empty list from reading as "all fine".
const showCoverage = computed(() => summary.value != null && summary.value.pgss_available)

// Monitoring queries and statements an index already serves are outcomes, not
// gaps: on a polled database they are most of the tally.
const BENIGN_REASONS = new Set(['system_relation', 'already_indexed'])
const hasRealGaps = computed(() =>
  notParsed.value.some(n => !BENIGN_REASONS.has(n.reason_code)),
)

const coverageText = computed(() => {
  const s = summary.value
  if (!s) return ''
  return t('indexes.advisor.coverage', {
    analyzed: fmtInt(s.analyzed_queries),
    groups: fmtInt(s.collapsed_groups),
    covered: fmtPct(s.covered_time_pct),
  })
})

// queryid, not search: the report is the top of each metric, and a covered
// statement usually leads none of them.
function queryLink(q: IndexAdvisorCoveredQuery) {
  return {
    name: 'query-report',
    params: { clustername: clusterName.value ?? '' },
    query: {
      ...(hostName.value ? { host: hostName.value } : {}),
      ...(databaseName.value ? { db: databaseName.value } : {}),
      queryid: q.query_ids[0],
    },
  }
}

const sqlDialogVisible = ref(false)
const sqlDialogText = ref('')
const sqlDialogQueryId = ref('')

function showSql(q: IndexAdvisorCoveredQuery) {
  sqlDialogQueryId.value = q.query_ids.join(', ')
  sqlDialogText.value = q.query
  sqlDialogVisible.value = true
}
</script>

<template>
  <v-card v-if="!unavailable" class="mb-4">
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <v-icon start icon="mdi-lightbulb-on-outline" /><span>{{ t('indexes.advisor.title') }}</span>
      <v-tooltip :text="t('indexes.advisor.hint')" location="bottom" max-width="480">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
      <v-spacer />
      <span v-if="durationMs" class="text-caption text-medium-emphasis">
        {{ t('indexes.advisor.duration', { ms: fmtInt(durationMs) }) }}
      </span>
    </v-card-title>
    <v-card-text>
      <v-alert type="info" variant="tonal" density="compact" class="mb-3" icon="mdi-information-outline">
        {{ t('indexes.advisor.disclaimer') }}
      </v-alert>

      <v-alert
        v-if="summary && !summary.pgss_available"
        type="warning"
        variant="tonal"
        density="compact"
        class="mb-3"
      >
        {{ t('indexes.advisor.pgssUnavailable') }}
      </v-alert>

      <v-alert
        v-if="summary?.catalog_truncated"
        type="warning"
        variant="tonal"
        density="compact"
        class="mb-3"
      >
        {{ t('indexes.advisor.catalogTruncated') }}
      </v-alert>

      <template v-if="showCoverage">
        <!-- Only an unread part of the workload is worth an alert. -->
        <v-alert v-if="hasRealGaps" type="warning" variant="tonal" density="compact" class="mb-3">
          <div>{{ coverageText }}</div>
          <div class="mt-1">{{ t('indexes.advisor.notParsedCount', { n: fmtInt(notParsedTotal) }) }}</div>
          <div v-for="n in notParsed" :key="n.reason_code" class="text-body-2 ml-2">
            <span class="font-weight-medium">{{ fmtInt(n.count) }}</span> — {{ reasonText(n) }}
          </div>
        </v-alert>
        <div v-else class="text-caption text-medium-emphasis mb-3">
          <div>{{ coverageText }}</div>
          <div v-for="n in notParsed" :key="n.reason_code" class="ml-2">
            {{ fmtInt(n.count) }} — {{ reasonText(n) }}
          </div>
        </div>
      </template>

      <v-data-table
        v-if="pgssReadable"
        :headers="headers"
        :items="candidates"
        :loading="loading"
        :item-value="rowKey"
        :no-data-text="t('indexes.advisor.noCandidates')"
        show-expand
      >
        <template #item.table="{ item }">
          <router-link :to="describeLink(item.schema, item.table)" class="text-decoration-none">{{ item.table }}</router-link>
        </template>
        <template #item.columns="{ item }">
          <span class="text-mono">({{ item.columns.join(', ') }})</span>
        </template>
        <template #item.weight_pct="{ item }">
          <div class="d-flex align-center ga-2">
            <span>{{ fmtPct(item.weight_pct) }}</span>
            <v-progress-linear
              :model-value="item.weight_pct"
              color="primary"
              height="4"
              rounded
              style="min-width: 60px; max-width: 90px"
            />
            <v-chip v-if="!item.planner_checked" size="x-small" variant="tonal" color="grey">
              {{ t('indexes.advisor.heuristic') }}
            </v-chip>
          </div>
        </template>
        <template #item.table_rows="{ value }">{{ fmtCompact(value) }}</template>
        <template #item.warnings="{ item }">
          <v-chip
            v-for="w in item.warnings"
            :key="w.code"
            size="x-small"
            variant="tonal"
            class="mr-1"
            :color="WARNING_COLOR[w.code]"
          >
            {{ warningLabel(w) }}
          </v-chip>
        </template>
        <template #expanded-row="{ columns, item }">
          <tr>
            <td :colspan="columns.length" class="py-3">
              <div class="d-flex align-center ga-2 mb-1">
                <span class="text-caption font-weight-medium">{{ t('indexes.advisor.ddl') }}</span>
                <v-btn icon="mdi-content-copy" variant="text" size="x-small" @click="copyToClipboard(item.ddl)" />
              </div>
              <pre class="sql-highlight sql-code text-mono text-body-2 mb-3" v-html="highlightSql(item.ddl)"></pre>

              <div v-if="item.warnings.length" class="mb-3">
                <div v-for="w in item.warnings" :key="w.code" class="text-caption text-medium-emphasis d-flex ga-1">
                  <v-icon size="x-small" :color="WARNING_COLOR[w.code]">mdi-alert-outline</v-icon>
                  <span>{{ warningText(w) }}</span>
                </div>
              </div>

              <div class="text-caption text-medium-emphasis mb-3">
                {{ t('indexes.advisor.writeCost', {
                  inserted: fmtCompact(item.writes.inserted),
                  updated: fmtCompact(item.writes.updated),
                  deleted: fmtCompact(item.writes.deleted),
                  seq: fmtCompact(item.writes.seq_scans),
                  idx: fmtCompact(item.writes.idx_scans),
                }) }}
              </div>

              <div class="text-caption font-weight-medium mb-1">{{ t('indexes.advisor.coveredQueries') }}</div>
              <v-table density="compact">
                <thead>
                  <tr>
                    <th>queryid</th>
                    <th>{{ t('indexes.advisor.weight') }}</th>
                    <th>{{ t('header.calls') }}</th>
                    <th>SQL</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="q in item.covered_queries" :key="q.fingerprint">
                    <td>
                      <router-link v-if="q.query_ids.length" :to="queryLink(q)" class="text-decoration-none text-mono">
                        {{ q.query_ids.join(', ') }}
                      </router-link>
                      <span v-else class="text-mono">—</span>
                    </td>
                    <td>{{ fmtPct(q.weight_pct) }}</td>
                    <td>{{ fmtCompact(q.calls) }}</td>
                    <td>
                      <div class="d-flex align-center">
                        <code class="sql-highlight text-mono text-body-2 text-medium-emphasis" v-html="highlightSql(truncateSql(q.query))"></code>
                        <v-btn size="small" variant="text" class="ml-1 flex-shrink-0" @click="showSql(q)">
                          {{ t('report.showSql') }}
                        </v-btn>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </td>
          </tr>
        </template>
      </v-data-table>
      <PaginationControls v-if="pgssReadable" :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>

    <SqlDialog v-model="sqlDialogVisible" :query-id="sqlDialogQueryId" :sql="sqlDialogText" />
  </v-card>
</template>
