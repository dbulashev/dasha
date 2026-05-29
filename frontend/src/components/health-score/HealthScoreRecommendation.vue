<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import {
  getHealthScoreAnalyzeDisabledTables,
  getHealthScoreHighDeadRatioTables,
  getHealthScoreHorizonBlockingSessions,
  getHealthScoreLowHotUpdateTables,
  getHealthScoreTablesAutovacuumOff,
  getHealthScoreXidWraparoundDatabases,
} from '@/api/gen/default/default'
import type { HealthScoreRecommendation } from '@/api/models/index'
import { assertOk } from '@/utils/api'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { INLINE_SPECS, RULES_WITH_INLINE_DETAILS } from './inlineDetails'

const props = defineProps<{
  rec: HealthScoreRecommendation
}>()

const { t, te } = useI18n()
const route = useRoute()
const { clusterName, hostName, databaseName } = useClusterInfo()

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
  // metric_pct: same value rendered as a percentage with one decimal,
  // for rules whose metric_value is a 0..1 ratio (HOT, newpage, requested_checkpoints,
  // lock_pool_saturation). Locale strings choose which form to use.
  return {
    metric_value: raw,
    metric_pct: Number.isFinite(raw) ? (raw * 100).toFixed(1) : raw,
    ...(props.rec.context ?? {}),
  }
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

const inlineRows = ref<Record<string, unknown>[]>([])
const inlineLoading = ref(false)
const inlineError = ref<string | null>(null)

async function loadInline() {
  if (!hasInline.value) return
  if (!clusterName.value || !hostName.value) return
  const spec = inlineSpec.value
  if (!spec) return
  if (spec.needsDatabase && !databaseName.value) return

  inlineLoading.value = true
  inlineError.value = null

  try {
    const cluster = clusterName.value
    const host = hostName.value
    const db = databaseName.value ?? ''

    let res
    switch (props.rec.rule_id) {
      case 'xid_wraparound_risk':
        res = await getHealthScoreXidWraparoundDatabases({ cluster_name: cluster, instance: host })
        break
      case 'tables_with_autovacuum_off':
        res = await getHealthScoreTablesAutovacuumOff({
          cluster_name: cluster,
          instance: host,
          database: db,
        })
        break
      case 'analyze_disabled_tables':
        res = await getHealthScoreAnalyzeDisabledTables({
          cluster_name: cluster,
          instance: host,
          database: db,
        })
        break
      case 'low_hot_update_ratio':
        res = await getHealthScoreLowHotUpdateTables({
          cluster_name: cluster,
          instance: host,
          database: db,
        })
        break
      case 'high_max_dead_ratio':
        res = await getHealthScoreHighDeadRatioTables({
          cluster_name: cluster,
          instance: host,
          database: db,
        })
        break
      case 'horizon_lag_xids':
        res = await getHealthScoreHorizonBlockingSessions({
          cluster_name: cluster,
          instance: host,
        })
        break
      default:
        return
    }

    const data = assertOk<unknown[]>(res)
    inlineRows.value = (data ?? []) as Record<string, unknown>[]
  } catch (err) {
    inlineError.value = err instanceof Error ? err.message : String(err)
    inlineRows.value = []
  } finally {
    inlineLoading.value = false
  }
}

watch(expanded, (open) => {
  if (open && hasInline.value && inlineRows.value.length === 0 && !inlineError.value) {
    void loadInline()
  }
})

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

          <!-- Inline data: small typed table fetched from a details endpoint. -->
          <template v-if="hasInline && inlineSpec">
            <div class="text-caption text-medium-emphasis mb-1">
              {{ t('healthScore.inline.title') }}
            </div>
            <v-progress-linear v-if="inlineLoading" indeterminate height="2" />
            <v-alert
              v-else-if="inlineError"
              type="error"
              variant="tonal"
              density="compact"
              class="mt-1"
            >
              {{ inlineError }}
            </v-alert>
            <v-alert
              v-else-if="inlineSpec.needsDatabase && !databaseName"
              type="info"
              variant="tonal"
              density="compact"
              class="mt-1"
            >
              {{ t('healthScore.inline.pickDatabase') }}
            </v-alert>
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
