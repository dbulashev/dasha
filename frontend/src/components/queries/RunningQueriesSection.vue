<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQueriesRunning, getDatabaseUsers } from '@/api/gen/default/default'
import type { QueryRunning } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import AutoRefreshControls from '@/components/AutoRefreshControls.vue'
import { useDebouncedRef } from '@/composables/useDebouncedRef'
import { useViewError } from '@/composables/useViewError'
import { useActiveQueriesStore } from '@/stores/activeQueries'
import { fmtMs as fmtMsUtil } from '@/utils/format'
import { highlightSql, copyToClipboard, truncateSql, SQL_PREVIEW_MAX } from '@/utils/sql'
import SqlDialog from '@/components/queries/SqlDialog.vue'
import '@/assets/sql-highlight.css'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()
const store = useActiveQueriesStore()

// Lexicographic order puts 10.0.0.2 ahead of 9.0.0.1; pad the octets so the
// column sorts the way an address list is read. Anything not dotted-quad (IPv6,
// the empty local/background case) keeps its plain string order.
function addrSortKey(addr: string | undefined): string {
  if (!addr) return ''
  const octets = addr.split('.')
  if (octets.length !== 4 || octets.some(o => !/^\d{1,3}$/.test(o))) return addr
  return octets.map(o => o.padStart(3, '0')).join('.')
}

const headers = computed(() => [
  { title: t('header.pid'), key: 'Pid' },
  // Address plus application_name in one cell, sorted by address so a noisy host groups together.
  {
    title: t('header.source'),
    key: 'ClientAddr',
    sortRaw: (a: QueryRunning, b: QueryRunning) =>
      addrSortKey(a.ClientAddr).localeCompare(addrSortKey(b.ClientAddr)),
  },
  { title: t('header.user'), key: 'User' },
  { title: t('header.state'), key: 'State' },
  { title: t('header.duration'), key: 'DurationMs' },
  // Sorted by type, so sessions stuck on the same kind of wait end up next to each other.
  { title: t('header.waitEvent'), key: 'WaitEventType' },
  { title: t('header.backendType'), key: 'BackendType' },
])

function fmtMs(ms: number | null | undefined): string {
  return fmtMsUtil(ms, t)
}

// Waiting is the contract's own verdict — every wait_event_type except the idle
// background (Client, Timeout, Activity). Don't restate the rule here.
function waitClass(item: QueryRunning): string {
  return item.Waiting ? 'text-warning' : 'text-medium-emphasis'
}

// PostgreSQL names a wait by both parts, e.g. Lock: transactionid. Before 9.6
// the server reports no name at all, only the boolean.
function waitText(item: QueryRunning): string {
  if (!item.WaitEventType) return item.Waiting ? t('queries.waitUnnamed') : '—'
  return item.WaitEvent ? `${item.WaitEventType}: ${item.WaitEvent}` : item.WaitEventType
}

// client_addr is NULL both for a unix socket and for a background worker that
// never had a client — only the former is "local".
function clientAddrText(item: QueryRunning): string {
  if (item.ClientAddr) return item.ClientAddr
  // BackendType is empty before PG 10, where every visible backend is a client one.
  return !item.BackendType || item.BackendType === 'client backend' ? t('queries.clientLocal') : '—'
}

function stateColor(state: string | undefined): string {
  if (state === 'idle in transaction') return 'warning'
  if (state === 'idle in transaction (aborted)') return 'error'
  return 'default'
}

// Vuetify v-data-table expects string keys; coerce Pid (number) so :expanded matches :item-value.
const itemKey = (item: QueryRunning) => String(item.Pid)

const sqlDialogVisible = ref(false)
const sqlDialogSql = ref('')
const sqlDialogPid = ref('')

// Inspecting the SQL is an explicit user action — pause auto-refresh so the row doesn't disappear mid-read.
function showSqlDialog(item: QueryRunning) {
  autoRefresh.stop()
  sqlDialogPid.value = String(item.Pid)
  sqlDialogSql.value = item.Query
  sqlDialogVisible.value = true
}

function copySql(sql: string) {
  autoRefresh.stop()
  copyToClipboard(sql)
}
const durationOptions = [0, 1, 5, 10, 50, 100, 500, 1000, 5000, 10000, 50000, 100000]
const intervalOptions = [1, 5, 10]

const cluster = computed(() => clusterName.value ?? '')

const minDuration = computed({
  get: () => store.get(cluster.value).minDuration,
  set: (v) => store.patch(cluster.value, { minDuration: v }),
})

const queryFilter = computed({
  get: () => store.get(cluster.value).queryFilter,
  set: (v) => store.patch(cluster.value, { queryFilter: v }),
})

const queryFilterDebounced = useDebouncedRef(queryFilter, 300)

const queryFilterMode = computed<'like' | 'not_like'>({
  get: () => store.get(cluster.value).queryFilterMode,
  set: (v) => store.patch(cluster.value, { queryFilterMode: v }),
})

const username = computed<string | null>({
  get: () => store.get(cluster.value).username,
  set: (v) => store.patch(cluster.value, { username: v }),
})

const intervalSec = computed({
  get: () => store.get(cluster.value).intervalSec,
  set: (v) => store.patch(cluster.value, { intervalSec: v }),
})

const { items: usersList } = useApiLoader<string[]>(
  () => getDatabaseUsers({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: () => {},
  },
)

const { items, loading, load } = useApiLoader<QueryRunning[]>(
  () => getQueriesRunning({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
    min_duration: minDuration.value,
    query_filter: queryFilterDebounced.value || undefined,
    query_filter_mode: queryFilterMode.value,
    username: username.value || undefined,
  }),
  {
    deps: [clusterName, hostName, databaseName, minDuration, queryFilterDebounced, queryFilterMode, username],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError,
  },
)

const autoRefresh = useAutoRefresh({
  pollInterval: () => intervalSec.value * 1000,
  onTick: () => load(),
})

// Stop auto-refresh on cluster switch — user explicitly opts in via Play (same as Progress).
watch(clusterName, () => autoRefresh.stop())

// Restart timer with new interval if currently running.
watch(intervalSec, () => autoRefresh.restart())
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <v-icon start icon="mdi-play-circle-outline" />{{ t('Live Queries') }}
      <AutoRefreshControls
        v-model:interval-sec="intervalSec"
        :active="autoRefresh.active.value"
        :remaining="autoRefresh.remaining.value"
        :loading="loading"
        :interval-options="intervalOptions"
        @toggle="autoRefresh.toggle"
        @refresh="load"
      />
      <v-spacer />
      <v-select
        v-model="minDuration"
        :items="durationOptions"
        :label="t('queries.minDurationLabel')"
        density="compact"
        hide-details
        style="max-width: 200px"
      />
    </v-card-title>

    <v-card-text>
      <v-row dense class="mb-2">
        <v-col cols="12" md="8">
          <div class="d-flex ga-2 align-center">
            <v-btn-toggle
              v-model="queryFilterMode"
              mandatory
              density="compact"
              variant="outlined"
              divided
            >
              <v-btn value="like" size="small">{{ t('queries.queryFilterModeLike') }}</v-btn>
              <v-btn value="not_like" size="small">{{ t('queries.queryFilterModeNotLike') }}</v-btn>
            </v-btn-toggle>
            <v-text-field
              v-model="queryFilter"
              :label="t('queries.queryFilterLabel')"
              :placeholder="t('queries.queryFilterPlaceholder')"
              density="compact"
              hide-details
              clearable
            />
          </div>
        </v-col>
        <v-col cols="12" md="4">
          <v-autocomplete
            v-model="username"
            :items="usersList"
            :label="t('queries.usernameLabel')"
            density="compact"
            hide-details
            clearable
          />
        </v-col>
      </v-row>

      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
        :expanded="items.map(itemKey)"
        :item-value="itemKey"
      >
        <template #item.ClientAddr="{ item }">
          <span :class="{ 'text-medium-emphasis': !item.ClientAddr }">{{ clientAddrText(item) }}</span>
          <span v-if="item.Source" class="text-medium-emphasis"> ({{ item.Source }})</span>
        </template>
        <template #item.State="{ item }">
          <v-chip size="small" variant="tonal" :color="stateColor(item.State)">{{ item.State }}</v-chip>
        </template>
        <template #item.DurationMs="{ item }">{{ fmtMs(item.DurationMs) }}</template>
        <template #item.WaitEventType="{ item }">
          <span :class="waitClass(item)">{{ waitText(item) }}</span>
        </template>
        <template #expanded-row="{ columns, item }">
          <tr v-if="item.Query" class="running-expanded-row">
            <td :colspan="columns.length" class="py-1 expanded-cell">
              <div class="d-flex align-center">
                <v-icon size="x-small" class="mr-1 text-medium-emphasis">mdi-subdirectory-arrow-right</v-icon>
                <code
                  class="sql-highlight text-mono text-body-2 text-medium-emphasis flex-grow-1 sql-truncate"
                  v-html="highlightSql(truncateSql(item.Query))"
                />
                <v-btn icon="mdi-content-copy" variant="text" size="x-small" class="ml-1 flex-shrink-0" @click="copySql(item.Query)" />
                <v-btn v-if="item.Query.length > SQL_PREVIEW_MAX" size="small" variant="text" class="ml-1 flex-shrink-0" @click="showSqlDialog(item)">
                  {{ t('report.showSql') }}
                </v-btn>
              </div>
            </td>
          </tr>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>

  <SqlDialog v-model="sqlDialogVisible" :query-id="sqlDialogPid" :sql="sqlDialogSql" :label="t('header.pid')" />
</template>

<style scoped>
.sql-truncate {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
  flex: 1 1 0;
}

.running-expanded-row .expanded-cell {
  background-color: rgba(var(--v-theme-on-surface), 0.02);
  max-width: 0;
}

.running-expanded-row .expanded-cell > div {
  min-width: 0;
}
</style>
