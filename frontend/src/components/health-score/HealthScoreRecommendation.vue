<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import {
  getHealthScoreHighDeadRatioTables,
  getHealthScoreHorizonBlockingSessions,
  getHealthScoreLowHotUpdateTables,
  getHealthScoreTablesAutovacuumOff,
  getHealthScoreXidWraparoundDatabases,
} from '@/api/gen/default/default'
import type { HealthScoreRecommendation } from '@/api/models/index'
import { fmtNum } from '@/utils/format'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { usePaginatedApiLoader } from '@/composables/useApiLoader'
import { DEFAULT_PAGE_SIZE } from '@/constants/pagination'
import PaginationControls from '@/components/PaginationControls.vue'
import { INLINE_SPECS, RULES_WITH_INLINE_DETAILS } from './inlineDetails'

const props = defineProps<{
  rec: HealthScoreRecommendation
  // Drill-down DB from ?database= query param, set when a row in the
  // Databases table is selected. Distinct from the global ?db= selector
  // (useClusterInfo.databaseName). When present, inline-detail per-DB
  // queries use this — otherwise we fall back to the global selector so
  // a standalone recommendation card still works without drill-down.
  database?: string | null
}>()

const { t, te } = useI18n()
const route = useRoute()
const { clusterName, hostName, databaseName } = useClusterInfo()

const effectiveDatabase = computed(() => props.database || databaseName.value || '')

const expanded = ref(false)

// Backend returns plain paths like "/queries" or "/queries?tab=stats".
// Wrap them with the current cluster/host/db context so the link lands on
// the right view, matching the pattern used by App.vue#withQuery.
const relatedLink = computed(() => {
  const raw = props.rec.related_route
  if (!raw) return null

  const [path, search] = raw.split('?')
  const cluster = (route.params.clustername as string) ?? ''
  const host = route.query.host ? String(route.query.host) : null
  const db = route.query.db ? String(route.query.db) : null

  const query: Record<string, string> = {}
  if (search) {
    for (const [k, v] of new URLSearchParams(search)) query[k] = v
  }
  if (host) query.host = host
  if (db) query.db = db

  return {
    path: `${path}/${cluster}`,
    query,
  }
})

const i18nBase = computed(() => `healthScore.recommendations.${props.rec.rule_id}`)

const i18nContext = computed<Record<string, unknown>>(() => {
  const raw = props.rec.metric_value
  // Round for display: raw metric/context values can carry full float precision
  // (e.g. "90.22492448754167%"). metric_pct: same value as a percentage with one
  // decimal, for rules whose metric_value is a 0..1 ratio (HOT, newpage,
  // requested_checkpoints, lock_pool_saturation). Locale strings choose the form.
  const ctx: Record<string, unknown> = {
    metric_value: fmtNum(raw),
    metric_pct: Number.isFinite(raw) ? (raw * 100).toFixed(1) : raw,
  }
  for (const [key, val] of Object.entries(props.rec.context ?? {})) {
    ctx[key] = fmtNum(val)
  }
  return ctx
})

const title = computed(() => t(`${i18nBase.value}.title`, i18nContext.value))
const short = computed(() => t(`${i18nBase.value}.short`, i18nContext.value))
const hasDetail = computed(() => te(`${i18nBase.value}.detail`))
const detail = computed(() =>
  hasDetail.value ? t(`${i18nBase.value}.detail`, i18nContext.value) : '',
)

// Inline detail support: a rule listed in RULES_WITH_INLINE_DETAILS fetches
// a small typed dataset from the matching /api/.../details endpoint when
// expanded. For these rules the i18n `sql` snippet is intentionally hidden
// — the data is more useful than the query that produced it. ALTER SYSTEM
// "fix" rules (autovacuum_disabled, track_counts_disabled,
// track_io_timing_disabled) keep showing the SQL since it IS the action.
const hasInline = computed(() => RULES_WITH_INLINE_DETAILS.has(props.rec.rule_id))
const inlineSpec = computed(() => INLINE_SPECS[props.rec.rule_id])

const hasSql = computed(() => !hasInline.value && te(`${i18nBase.value}.sql`))
const sql = computed(() => (hasSql.value ? t(`${i18nBase.value}.sql`, i18nContext.value) : ''))

const inlineError = ref<string | null>(null)

// Dispatch the inline-detail request for the current rule. Lists can be long
// (e.g. every table over the dead-tuple threshold), so each endpoint is paged
// with limit/offset rather than capped server-side.
function inlineFetcher(
  limit: number,
  offset: number,
): Promise<{ data: unknown; status: number }> {
  const cluster = clusterName.value as string
  const host = hostName.value as string
  const db = effectiveDatabase.value

  switch (props.rec.rule_id) {
    case 'xid_wraparound_risk':
      return getHealthScoreXidWraparoundDatabases({ cluster_name: cluster, instance: host, limit, offset })
    case 'tables_with_autovacuum_off':
      return getHealthScoreTablesAutovacuumOff({ cluster_name: cluster, instance: host, database: db, limit, offset })
    case 'low_hot_update_ratio':
      return getHealthScoreLowHotUpdateTables({ cluster_name: cluster, instance: host, database: db, limit, offset })
    case 'high_max_dead_ratio':
      return getHealthScoreHighDeadRatioTables({ cluster_name: cluster, instance: host, database: db, limit, offset })
    case 'horizon_lag_xids':
      return getHealthScoreHorizonBlockingSessions({ cluster_name: cluster, instance: host, limit, offset })
    default:
      // The hasInline guard means this rule is listed in RULES_WITH_INLINE_DETAILS
      // but has no fetcher case here — contract drift. Fail loudly instead of
      // silently rendering an empty list.
      return Promise.reject(
        new Error(
          `No inline-detail fetcher for rule "${props.rec.rule_id}" ` +
            `(cluster=${cluster}, instance=${host}, database=${db || '-'})`,
        ),
      )
  }
}

// Paginated, lazy: only fetches once the card is expanded and the request
// context is complete. Any change to host/database/rule (deps) resets to page 1;
// collapsing skips the fetch and re-expanding reloads fresh data.
const {
  items: inlineRows,
  loading: inlineLoading,
  page,
  hasMore,
  load,
} = usePaginatedApiLoader<Record<string, unknown>>(
  (limit, offset) => {
    inlineError.value = null
    return inlineFetcher(limit, offset)
  },
  {
    pageSize: DEFAULT_PAGE_SIZE,
    deps: [expanded, clusterName, hostName, effectiveDatabase, () => props.rec.rule_id],
    guard: () =>
      expanded.value &&
      hasInline.value &&
      !!clusterName.value &&
      !!hostName.value &&
      (!inlineSpec.value?.needsDatabase || !!effectiveDatabase.value),
    onError: (msg) => {
      inlineError.value = msg
    },
  },
)

const showExpander = computed(() => hasDetail.value || hasSql.value || hasInline.value)

const severityColor = computed(() => {
  switch (props.rec.severity) {
    case 'HIGH':
      return 'error'
    case 'MEDIUM':
      return 'warning'
    case 'LOW':
      return 'info'
    default:
      return 'default'
  }
})

const severityIcon = computed(() => {
  switch (props.rec.severity) {
    case 'HIGH':
      return 'mdi-alert-octagon'
    case 'MEDIUM':
      return 'mdi-alert'
    case 'LOW':
      return 'mdi-information-outline'
    default:
      return 'mdi-circle-outline'
  }
})

const copied = ref(false)

async function copySql() {
  try {
    await navigator.clipboard.writeText(sql.value)
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 1500)
  } catch {
    // Clipboard API unavailable; ignore.
  }
}
</script>

<template>
  <v-card variant="outlined" class="mb-2">
    <v-card-text class="pa-3">
      <div class="d-flex align-center ga-2">
        <v-chip :color="severityColor" variant="flat" size="small" :prepend-icon="severityIcon">
          {{ rec.severity }}
        </v-chip>
        <span class="text-body-1 font-weight-medium">{{ title }}</span>
        <v-spacer />
        <v-btn
          v-if="relatedLink && !hasInline"
          variant="text"
          size="small"
          :to="relatedLink"
          append-icon="mdi-arrow-right"
        >
          {{ t('healthScore.recommendations.openRelated') }}
        </v-btn>
      </div>

      <div class="text-body-2 text-medium-emphasis mt-2">{{ short }}</div>

      <v-expand-transition>
        <div v-if="expanded" class="mt-3">
          <div v-if="detail" class="text-body-2 mb-2" style="white-space: pre-line">
            {{ detail }}
          </div>

          <!-- Inline data: typed, paginated table fetched from a details endpoint. -->
          <template v-if="hasInline && inlineSpec">
            <div class="text-caption text-medium-emphasis mb-1">
              {{ t('healthScore.inline.title') }}
            </div>
            <v-alert
              v-if="inlineError"
              type="error"
              variant="tonal"
              density="compact"
              class="mt-1"
            >
              {{ inlineError }}
            </v-alert>
            <v-alert
              v-else-if="inlineSpec.needsDatabase && !effectiveDatabase"
              type="info"
              variant="tonal"
              density="compact"
              class="mt-1"
            >
              {{ t('healthScore.inline.pickDatabase') }}
            </v-alert>
            <template v-else>
              <v-progress-linear v-if="inlineLoading" indeterminate height="2" class="mt-1" />
              <v-alert
                v-else-if="!inlineRows.length"
                type="success"
                variant="tonal"
                density="compact"
                class="mt-1"
              >
                {{ t('healthScore.inline.empty') }}
              </v-alert>
              <v-table v-else density="compact" class="inline-table rounded mt-1">
                <thead>
                  <tr>
                    <th
                      v-for="col in inlineSpec.columns(t)"
                      :key="col.key"
                      class="text-caption"
                    >
                      {{ col.title }}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(row, i) in inlineRows" :key="i">
                    <td
                      v-for="col in inlineSpec.columns(t)"
                      :key="col.key"
                      :class="col.cellClass"
                    >
                      {{ col.format ? col.format(row[col.key]) : row[col.key] }}
                    </td>
                  </tr>
                </tbody>
              </v-table>
              <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
            </template>
          </template>

          <!-- Plain SQL block: only for fix commands (ALTER SYSTEM) now. -->
          <div v-if="sql" class="d-flex align-start ga-2 mt-2">
            <pre class="rec-sql flex-grow-1"><code>{{ sql }}</code></pre>
            <v-btn
              size="small"
              variant="tonal"
              :prepend-icon="copied ? 'mdi-check' : 'mdi-content-copy'"
              @click="copySql"
            >
              {{ copied ? t('healthScore.recommendations.copied') : t('healthScore.recommendations.copy') }}
            </v-btn>
          </div>
        </div>
      </v-expand-transition>

      <div v-if="showExpander" class="mt-2">
        <v-btn
          variant="text"
          size="small"
          :append-icon="expanded ? 'mdi-chevron-up' : 'mdi-chevron-down'"
          @click="expanded = !expanded"
        >
          {{ expanded
            ? t('healthScore.recommendations.hideHowTo')
            : t('healthScore.recommendations.showHowTo') }}
        </v-btn>
      </div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.rec-sql {
  background: rgba(0, 0, 0, 0.04);
  padding: 8px 12px;
  border-radius: 4px;
  font-size: 0.85em;
  overflow-x: auto;
  margin: 0;
}
.inline-table :deep(.inline-query-cell) {
  font-family: 'Roboto Mono', ui-monospace, monospace;
  font-size: 0.8em;
  white-space: pre-wrap;
  word-break: break-word;
  max-width: 420px;
}
.inline-table :deep(td) {
  font-size: 0.85em;
}
</style>
