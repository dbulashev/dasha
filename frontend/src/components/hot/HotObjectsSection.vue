<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getTablesHot, getIndexesHot } from '@/api/gen/default/default'
import type { HotReport, HotEntry } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useViewError } from '@/composables/useViewError'
import { ApiError, assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtBytes, fmtCompact, fmtDateTime, fmtInt } from '@/utils/format'
import { useDescribeLink } from '@/composables/useDescribeLink'
import { usePrefsStore } from '@/stores/prefs'
import PaginationControls from '@/components/PaginationControls.vue'

const props = defineProps<{ kind: 'table' | 'index' }>()

const { clusterName, databaseName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()
const { describeLink } = useDescribeLink()
const prefs = usePrefsStore()

type HotClass = 'reads' | 'writes' | 'io'

const classOptions = computed(() =>
  (props.kind === 'table' ? ['reads', 'writes', 'io'] : ['reads', 'io']).map(c => ({
    value: c,
    title: t(`hot.class.${c}`),
  })),
)

// Counter columns per kind+class — raw pg_stat column names as labels (the
// audience reads them natively, and they match the API payload keys).
const CLASS_COUNTERS: Record<string, Record<string, string[]>> = {
  table: {
    reads: ['seq_tup_read', 'idx_tup_fetch', 'seq_scan', 'idx_scan'],
    writes: ['n_tup_ins', 'n_tup_upd', 'n_tup_del', 'n_tup_hot_upd'],
    io: ['heap_blks_read', 'idx_blks_read', 'toast_blks_read'],
  },
  index: {
    reads: ['idx_scan', 'idx_tup_read'],
    io: ['idx_blks_read', 'idx_blks_hit'],
  },
}

const selectedClass = ref<HotClass>('reads')
const selectedDate = ref<string | null>(null)

const report = ref<HotReport | null>(null)
const loading = ref(false)
const unavailable = ref(false) // 501: no snapshot storage — hide the section
const page = ref(1)
const hasMore = ref(true)

async function load(p = 1) {
  if (!clusterName.value || !databaseName.value) return
  loading.value = true
  try {
    const pageSize = prefs.pageSize
    const params = {
      cluster_name: clusterName.value,
      database: databaseName.value,
      class: selectedClass.value,
      at: selectedDate.value ?? undefined,
      limit: pageSize,
      offset: (p - 1) * pageSize,
    }
    const res = props.kind === 'table'
      ? await getTablesHot(params as Parameters<typeof getTablesHot>[0])
      : await getIndexesHot(params as Parameters<typeof getIndexesHot>[0])
    if (res.status === 404) {
      // Storage is there but no snapshot captured yet.
      report.value = null
      return
    }
    report.value = assertOk<HotReport>(res)
    page.value = p
    hasMore.value = (report.value?.entries.length ?? 0) >= pageSize
  } catch (err) {
    if (err instanceof ApiError && err.status === 501) {
      unavailable.value = true
      return
    }
    onError(getErrorMessage(err), err)
    report.value = null
  } finally {
    loading.value = false
  }
}

watch([clusterName, databaseName], () => {
  unavailable.value = false
  selectedDate.value = null
  load()
}, { immediate: true })

watch([selectedClass, selectedDate, () => prefs.pageSize], () => load())

const counterKeys = computed(() => CLASS_COUNTERS[props.kind][selectedClass.value] ?? [])

const headers = computed(() => [
  { title: '#', key: 'rank', width: 70 },
  { title: t(props.kind === 'table' ? 'header.table' : 'header.index'), key: 'object' },
  { title: t('header.size'), key: 'size_bytes' },
  { title: t('hot.ratePerDay'), key: 'rate_per_day' },
  ...counterKeys.value.map(k => ({ title: k, key: `delta.${k}`, sortable: false })),
])

const dateItems = computed(() => {
  const dates = report.value?.snapshot.dates ?? []
  const latest = { value: null as string | null, title: t('hot.latestSnapshot') }
  // Every capture is selectable: the `at` parameter targets the exact
  // timestamp, so values are unique regardless of how many snapshots share a
  // day (the demo lab captures every 5 minutes).
  const items = dates.map(d => ({ value: d as string | null, title: fmtDateTime(d) }))
  return [latest, ...items]
})

// A snapshot with no complete host window (the very first capture, which only
// seeds anchors, or a run where every host's stats epoch broke) carries no real
// deltas — its coverage defaults to a meaningless 100%. Treat it as "warming up"
// rather than showing an empty top under a bogus coverage chip.
const measured = computed(() => {
  const w = report.value?.snapshot.windows
  return w ? Object.values(w).some(x => x.complete) : false
})

const coveragePct = computed(() => {
  const c = report.value?.snapshot.coverage
  return c != null ? (c * 100).toFixed(1) : null
})

// Row key must be schema-qualified: two objects can share a name across schemas
// (public.orders / sales.orders), and a bare `object` key would collide and make
// show-expand toggle every row with that name at once.
function rowKey(item: HotEntry) {
  return `${item.schema}.${item.object}`
}

// Tail histogram (objects outside the stored top): deciles of the class key.
const tail = computed(() => report.value?.snapshot.histogram ?? null)

// A fully idle tail (max delta 0) collapses to one phrase: nine zero-height
// bars and "P50 0, P90 0, max 0" say the same thing with more ink.
const tailIdle = computed(() => tail.value != null && tail.value.max === 0)

const tailBars = computed(() => {
  const h = tail.value
  if (!h || !h.deciles.length) return []
  const maxVal = Math.max(...h.deciles, 1)
  return h.deciles.map((d, i) => ({
    pct: 10 * (i + 1),
    value: d,
    // 2px floor keeps zero/tiny deciles visible as "present but negligible".
    height: Math.max(2, Math.round((d / maxVal) * 28)),
  }))
})

const tailMedian = computed(() => tail.value?.deciles[4] ?? 0)
const tailP90 = computed(() => tail.value?.deciles[8] ?? 0)

function sortedHosts(item: HotEntry) {
  return [...item.per_host].sort((a, b) => a.instance.localeCompare(b.instance))
}

function trend(item: HotEntry): { icon: string; color: string; label: string } | null {
  if (item.prev_rank == null) return { icon: 'mdi-new-box', color: 'info', label: t('hot.trendNew') }
  if (item.prev_rank > item.rank) return { icon: 'mdi-arrow-up-thin', color: 'error', label: `${item.prev_rank} → ${item.rank}` }
  if (item.prev_rank < item.rank) return { icon: 'mdi-arrow-down-thin', color: 'success', label: `${item.prev_rank} → ${item.rank}` }
  return null
}

const title = computed(() => t(props.kind === 'table' ? 'hot.tablesTitle' : 'hot.indexesTitle'))
</script>

<template>
  <v-card v-if="!unavailable" class="mb-4">
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <v-icon start icon="mdi-fire" /><span>{{ title }}</span>
      <v-tooltip :text="t('hot.hint')" location="bottom" max-width="480">
        <template #activator="{ props: tp }">
          <v-icon v-bind="tp" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
      <v-chip v-if="coveragePct != null && measured" size="small" variant="tonal">
        {{ t('hot.coverage', { pct: coveragePct }) }}
      </v-chip>
      <v-spacer />
      <v-select
        v-model="selectedClass"
        :items="classOptions"
        :label="t('hot.classLabel')"
        density="compact"
        variant="outlined"
        hide-details
        style="max-width: 180px"
      />
      <v-select
        v-model="selectedDate"
        :items="dateItems"
        :label="t('hot.date')"
        density="compact"
        variant="outlined"
        hide-details
        style="max-width: 220px"
      />
    </v-card-title>
    <v-card-text>
      <v-alert
        v-if="report?.snapshot.hosts_missing?.length"
        type="warning"
        variant="tonal"
        density="compact"
        class="mb-3"
      >
        {{ t('hot.hostsMissing', { hosts: report!.snapshot.hosts_missing.join(', ') }) }}
      </v-alert>

      <template v-if="report && measured">
        <v-data-table
          :headers="headers"
          :items="report.entries"
          :loading="loading"
          :item-value="rowKey"
          show-expand
          items-per-page="-1"
          hide-default-footer
        >
          <template #item.rank="{ item }">
            <span class="d-inline-flex align-center ga-1 text-no-wrap">
              {{ item.rank }}
              <v-tooltip v-if="trend(item)" :text="trend(item)!.label" location="bottom">
                <template #activator="{ props: tp }">
                  <v-icon v-bind="tp" size="small" :color="trend(item)!.color">{{ trend(item)!.icon }}</v-icon>
                </template>
              </v-tooltip>
            </span>
          </template>
          <template #item.object="{ item }">
            <template v-if="kind === 'table'">
              <router-link :to="describeLink(item.schema, item.object)" class="text-decoration-none">
                {{ item.schema }}.{{ item.object }}
              </router-link>
            </template>
            <template v-else>
              <span class="text-mono">{{ item.object }}</span>
              <router-link
                v-if="item.table_name"
                :to="describeLink(item.schema, item.table_name)"
                class="text-caption text-medium-emphasis ml-1 text-decoration-none"
              >{{ item.schema }}.{{ item.table_name }}</router-link>
            </template>
          </template>
          <template #item.size_bytes="{ value }">{{ fmtBytes(value) }}</template>
          <template #item.rate_per_day="{ value }">
            <v-tooltip :text="fmtInt(Math.round(value))" location="bottom">
              <template #activator="{ props: tp }">
                <span v-bind="tp">{{ fmtCompact(Math.round(value)) }}</span>
              </template>
            </v-tooltip>
          </template>
          <template v-for="k in counterKeys" :key="k" #[`item.delta.${k}`]="{ item }">
            <v-tooltip :text="fmtInt(item.delta[k] ?? 0)" location="bottom">
              <template #activator="{ props: tp }">
                <span v-bind="tp">{{ fmtCompact(item.delta[k] ?? 0) }}</span>
              </template>
            </v-tooltip>
          </template>
          <template #expanded-row="{ columns, item }">
            <tr>
              <td :colspan="columns.length" class="py-2">
                <v-table density="compact">
                  <thead>
                    <tr>
                      <th>{{ t('hot.host') }}</th>
                      <th v-for="k in counterKeys" :key="k">{{ k }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="h in sortedHosts(item)" :key="h.instance">
                      <td>
                        <span class="d-inline-flex align-center ga-1">
                          <v-tooltip :text="t(h.in_recovery ? 'hot.roleReplica' : 'hot.rolePrimary')" location="bottom">
                            <template #activator="{ props: tp }">
                              <v-icon v-bind="tp" size="small" :color="h.in_recovery ? undefined : 'primary'">
                                {{ h.in_recovery ? 'mdi-database-sync-outline' : 'mdi-database' }}
                              </v-icon>
                            </template>
                          </v-tooltip>
                          {{ h.instance }}
                        </span>
                      </td>
                      <td v-for="k in counterKeys" :key="k">{{ fmtCompact(h.delta[k] ?? 0) }}</td>
                    </tr>
                  </tbody>
                </v-table>
              </td>
            </tr>
          </template>
        </v-data-table>
        <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />

        <div v-if="tail && tail.count > 0" class="d-flex align-center ga-3 flex-wrap mt-2">
          <template v-if="tailIdle">
            <span class="text-caption text-medium-emphasis d-inline-flex align-center ga-1">
              <v-icon size="small">mdi-chart-histogram</v-icon>
              {{ t('hot.tail.idle', { count: tail.count }) }}
            </span>
          </template>
          <template v-else>
            <v-tooltip :text="t('hot.tail.hint')" location="bottom" max-width="440">
              <template #activator="{ props: tp }">
                <span v-bind="tp" class="text-caption text-medium-emphasis d-inline-flex align-center ga-1">
                  <v-icon size="small">mdi-chart-histogram</v-icon>
                  {{ t('hot.tail.summary', {
                    count: tail.count,
                    sum: fmtCompact(tail.sum),
                    median: fmtCompact(tailMedian),
                    p90: fmtCompact(tailP90),
                    max: fmtCompact(tail.max),
                  }) }}
                </span>
              </template>
            </v-tooltip>
            <span class="tail-bars" aria-hidden="true">
              <v-tooltip v-for="b in tailBars" :key="b.pct" :text="`P${b.pct}: ${fmtCompact(b.value)}`" location="top">
                <template #activator="{ props: tp }">
                  <span v-bind="tp" class="tail-bar" :style="{ height: b.height + 'px' }" />
                </template>
              </v-tooltip>
            </span>
          </template>
        </div>
      </template>
      <div v-else-if="report && !measured" class="text-medium-emphasis">{{ t('hot.warmingUp') }}</div>
      <div v-else-if="!loading" class="text-medium-emphasis">{{ t('hot.noSnapshots') }}</div>
      <v-progress-linear v-else indeterminate />
    </v-card-text>
  </v-card>
</template>

<style scoped>
.tail-bars {
  display: inline-flex;
  align-items: flex-end;
  gap: 2px;
  height: 30px;
}

.tail-bar {
  width: 8px;
  border-radius: 2px 2px 0 0;
  background-color: rgba(var(--v-theme-primary), 0.55);
}
</style>
