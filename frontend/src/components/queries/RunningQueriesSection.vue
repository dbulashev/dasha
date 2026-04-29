<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQueriesRunning, getDatabaseUsers } from '@/api/gen/default/default'
import type { QueryRunning } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
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

const headers = computed(() => [
  { title: t('header.pid'), key: 'Pid' },
  { title: t('header.source'), key: 'Source' },
  { title: t('header.duration'), key: 'DurationMs' },
  { title: t('header.waiting'), key: 'Waiting' },
  { title: t('header.user'), key: 'User' },
  { title: t('header.backendType'), key: 'BackendType' },
])

function fmtMs(ms: number | null | undefined): string {
  return fmtMsUtil(ms, t)
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
      <v-btn
        :icon="autoRefresh.active.value ? 'mdi-stop' : 'mdi-play'"
        :color="autoRefresh.active.value ? 'error' : 'success'"
        :title="autoRefresh.active.value ? t('queries.stopTooltip') : t('queries.playTooltip')"
        variant="tonal"
        size="small"
        @click="autoRefresh.toggle"
      />
      <span v-if="autoRefresh.active.value" class="text-body-2 d-flex align-center ga-1">
        <v-icon size="small" color="success" class="auto-refresh-icon">mdi-refresh</v-icon>
        {{ autoRefresh.formatRemaining(autoRefresh.remaining.value) }}
      </span>
      <v-select
        v-model="intervalSec"
        :items="intervalOptions"
        :label="t('queries.intervalLabel')"
        density="compact"
        hide-details
        style="max-width: 110px"
      >
        <template #selection="{ item }">{{ t('queries.intervalSec', { n: item.raw }) }}</template>
        <template #item="{ item, props }">
          <v-list-item v-bind="props" :title="t('queries.intervalSec', { n: item.raw })" />
        </template>
      </v-select>
      <v-btn
        icon="mdi-refresh"
        :title="t('queries.refreshTooltip')"
        variant="text"
        size="small"
        :loading="loading"
        @click="load"
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
        <template #item.DurationMs="{ item }">{{ fmtMs(item.DurationMs) }}</template>
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
.auto-refresh-icon {
  animation: spin 2s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

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
