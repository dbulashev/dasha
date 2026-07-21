<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useViewError } from '@/composables/useViewError'
import { getIndexesUnusedReport } from '@/api/gen/default/default'
import type {
  IndexUnusedReport,
  IndexVerdict,
  IndexVerdictReasonParams,
} from '@/api/models/index'
import { GetIndexesUnusedReportVerdict } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useDebouncedRef } from '@/composables/useDebouncedRef'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtBytes } from '@/utils/format'
import { useDescribeLink } from '@/composables/useDescribeLink'
import { usePrefsStore } from '@/stores/prefs'
import PaginationControls from '@/components/PaginationControls.vue'

const prefs = usePrefsStore()

const { clusterName, databaseName } = useClusterInfo()
const { describeLink } = useDescribeLink()
const { t, te } = useI18n()
const { onError } = useViewError()

type VerdictFilter = GetIndexesUnusedReportVerdict

// Taken from the generated enum so a verdict added to the contract shows up here.
const VERDICTS = Object.values(GetIndexesUnusedReportVerdict)

// Nullable because `clearable` writes null, not '', when the field is cleared.
const verdictFilter = ref<VerdictFilter | null>(null)
const tableFilter = ref<string | null>('')
const indexFilter = ref<string | null>('')
// Typing a name must not fire a request per keystroke: the endpoint recomputes every
// verdict on the cluster before it can filter.
const tableQuery = useDebouncedRef(tableFilter, 400)
const indexQuery = useDebouncedRef(indexFilter, 400)

const items = ref<IndexVerdict[]>([])
const unreachableHosts = ref<string[]>([])
const loading = ref(false)
const page = ref(1)
const hasMore = ref(true)

// The endpoint deliberately takes no instance: idx_scan is per-instance and not
// replicated, so only the cluster-wide picture can justify a DROP. The response
// is an object (indexes + unreachable_hosts), hence no usePaginatedApiLoader.
// Guards against out-of-order responses: switching cluster/db or typing into a filter
// can leave an older request in flight whose late reply would clobber the newer one.
// Only the latest generation applies state or clears loading.
let reqId = 0

async function load(p = 1) {
  if (!clusterName.value || !databaseName.value) return
  const myId = ++reqId
  loading.value = true
  try {
    const pageSize = prefs.pageSize
    const res = await getIndexesUnusedReport({
      cluster_name: clusterName.value,
      database: databaseName.value,
      verdict: verdictFilter.value ?? undefined,
      table: tableQuery.value?.trim() || undefined,
      index: indexQuery.value?.trim() || undefined,
      limit: pageSize,
      offset: (p - 1) * pageSize,
    })
    const body = assertOk<IndexUnusedReport>(res)
    if (myId !== reqId) return // superseded — leave state to the newer load
    items.value = body?.indexes ?? []
    unreachableHosts.value = body?.unreachable_hosts ?? []
    page.value = p
    hasMore.value = items.value.length >= pageSize
  } catch (err) {
    if (myId !== reqId) return
    onError(getErrorMessage(err), err)
    items.value = []
    unreachableHosts.value = []
  } finally {
    if (myId === reqId) loading.value = false
  }
}

// Row key must be schema-qualified: an index name is unique only within its
// schema, so a bare `index` key would collide and make show-expand toggle every
// row with that name at once.
function rowKey(item: IndexVerdict) {
  return `${item.schema}.${item.table}.${item.index}`
}

watch(
  [clusterName, databaseName, verdictFilter, tableQuery, indexQuery, () => prefs.pageSize],
  () => load(),
  { immediate: true },
)

const headers = computed(() => [
  { title: t('header.schema'), key: 'schema' },
  { title: t('header.table'), key: 'table' },
  { title: t('header.index'), key: 'index' },
  { title: t('header.size'), key: 'size_bytes' },
  { title: t('indexes.report.verdict'), key: 'verdict' },
])

const verdictOptions = computed(() => [
  { value: null, title: t('indexes.report.allVerdicts') },
  ...VERDICTS.map(v => ({ value: v as VerdictFilter | null, title: t(`indexes.report.verdicts.${v}`) })),
])

const VERDICT_COLOR: Record<string, string> = {
  drop_candidate: 'success',
  used: 'info',
  stale_evidence: 'warning',
  insufficient_data: 'grey',
  unknown: 'error',
}

const VERDICT_ICON: Record<string, string> = {
  drop_candidate: 'mdi-delete-outline',
  used: 'mdi-check-circle-outline',
  stale_evidence: 'mdi-history',
  insufficient_data: 'mdi-timer-sand',
  unknown: 'mdi-help-circle-outline',
}

const fmtWindow = (days: number) => days >= 1 ? `${days.toFixed(1)} ${t('indexes.report.days')}` : `${(days * 24).toFixed(1)} ${t('time.h')}`

// The API ships the same explanation twice: `reason` as English prose and
// reason_code + reason_params as its localizable form. Build the sentence here, and
// fall back to the prose only if the backend grows a code this build does not know.
function reasonText(item: IndexVerdict): string {
  const key = `indexes.report.reasons.${item.reason_code}`
  if (!te(key)) return item.reason

  const p: IndexVerdictReasonParams = item.reason_params ?? {}
  const usedOn = (p.used_on ?? [])
    .map((h) => `${h.instance} (${t('indexes.report.rate', { n: h.scans_per_day.toFixed(1) })})`)
    .join(', ')

  // Every code gets every parameter — vue-i18n drops the ones its phrasing omits, and
  // which fields the backend fills is the code's business, not the renderer's.
  const parts = [t(key, {
    hosts: (p.hosts ?? []).join(', '),
    usedOn,
    window: (p.window_days ?? 0).toFixed(1),
    minWindow: Math.round(p.min_window_days ?? 0),
    scans: p.total_scans ?? 0,
    days: Math.round(p.window_days ?? 0),
    hostCount: p.host_count ?? 0,
  })]

  for (const note of item.reason_notes ?? []) {
    const noteKey = `indexes.report.notes.${note}`
    if (te(noteKey)) parts.push(t(noteKey, { n: item.partitions ?? 0 }))
  }

  return parts.join(' ')
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <v-icon start icon="mdi-scale-balance" /><span>{{ t('indexes.report.title') }}</span>
      <v-tooltip :text="t('indexes.report.hint')" location="bottom" max-width="480">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
      <v-spacer />
      <v-select
        v-model="verdictFilter"
        :items="verdictOptions"
        :label="t('indexes.report.verdict')"
        density="compact"
        variant="outlined"
        hide-details
        style="max-width: 220px"
      />
      <v-text-field
        v-model="tableFilter"
        :label="t('header.table')"
        prepend-inner-icon="mdi-magnify"
        density="compact"
        variant="outlined"
        clearable
        hide-details
        style="max-width: 200px"
      />
      <v-text-field
        v-model="indexFilter"
        :label="t('header.index')"
        prepend-inner-icon="mdi-magnify"
        density="compact"
        variant="outlined"
        clearable
        hide-details
        style="max-width: 200px"
      />
    </v-card-title>
    <v-card-text>
      <v-alert type="info" variant="tonal" density="compact" class="mb-3" icon="mdi-information-outline">
        {{ t('indexes.report.disclaimer') }}
      </v-alert>

      <v-alert v-if="unreachableHosts.length" type="warning" variant="tonal" density="compact" class="mb-3">
        {{ t('indexes.report.unreachable', { hosts: unreachableHosts.join(', ') }) }}
      </v-alert>

      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
        :item-value="rowKey"
        show-expand
      >
        <template #item.table="{ item }">
          <router-link :to="describeLink(item.schema, item.table)" class="text-decoration-none">{{ item.table }}</router-link>
        </template>
        <template #item.index="{ item }">
          <span class="text-mono">{{ item.index }}</span>
          <v-chip v-if="item.partitioned" size="x-small" variant="tonal" class="ml-1">
            {{ t('indexes.report.partitioned', { n: item.partitions ?? 0 }) }}
          </v-chip>
        </template>
        <template #item.size_bytes="{ value }">{{ fmtBytes(value) }}</template>
        <template #item.verdict="{ item }">
          <v-chip size="small" variant="tonal" :color="VERDICT_COLOR[item.verdict]" :prepend-icon="VERDICT_ICON[item.verdict]">
            {{ t(`indexes.report.verdicts.${item.verdict}`) }}
          </v-chip>
        </template>
        <template #expanded-row="{ columns, item }">
          <tr>
            <td :colspan="columns.length" class="py-2">
              <div class="text-caption text-medium-emphasis mb-2">
                <span class="font-weight-medium">{{ t('indexes.report.reason') }}:</span>
                {{ reasonText(item) }}
              </div>
              <v-table density="compact">
                <thead>
                  <tr>
                    <th>{{ t('indexes.report.host') }}</th>
                    <th>{{ t('header.indexScans') }}</th>
                    <th>{{ t('indexes.report.window') }}</th>
                    <th>{{ t('indexes.report.scansPerDay') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="h in item.per_instance" :key="h.instance">
                    <td>
                      <span class="d-inline-flex align-center ga-1">
                        <v-tooltip :text="t(h.in_recovery ? 'indexes.report.roleReplica' : 'indexes.report.rolePrimary')" location="bottom">
                          <template #activator="{ props }">
                            <v-icon v-bind="props" size="small" :color="h.in_recovery ? undefined : 'primary'">
                              {{ h.in_recovery ? 'mdi-database-sync-outline' : 'mdi-database' }}
                            </v-icon>
                          </template>
                        </v-tooltip>
                        {{ h.instance }}
                      </span>
                    </td>
                    <td>{{ h.index_scans }}</td>
                    <td>
                      {{ fmtWindow(h.window_days) }}
                      <v-tooltip v-if="!h.stats_reset_known" :text="t('indexes.report.statsResetUnknown')" location="bottom" max-width="380">
                        <template #activator="{ props }">
                          <v-icon v-bind="props" size="x-small" color="warning">mdi-alert-outline</v-icon>
                        </template>
                      </v-tooltip>
                    </td>
                    <td>{{ h.scans_per_day.toFixed(2) }}</td>
                  </tr>
                </tbody>
              </v-table>
            </td>
          </tr>
        </template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
