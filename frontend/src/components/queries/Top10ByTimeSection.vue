<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQueriesTop10ByTime } from '@/api/gen/default/default'
import type { QueryTop10ByTime } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { highlightSql, copyToClipboard } from '@/utils/sql'
import '@/assets/sql-highlight.css'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.queryId'), key: 'QueryID' },
  { title: t('header.execTime'), key: 'ExecTimeMs' },
  { title: t('header.ioCpuPct'), key: 'IoCpuPct', sortable: false },
  { title: t('header.queryTrunc'), key: 'QueryTrunc' },
])

const { items, loading } = useApiLoader<QueryTop10ByTime[]>(
  () => getQueriesTop10ByTime({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: (msg) => emit('error', msg),
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      {{ t('Top 10 by Execution Time') }}
      <v-tooltip :text="t('hint.queryStats')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading">
        <template #item.ExecTimeMs="{ item }">{{ item.ExecTime }}</template>
        <template #item.QueryID="{ value }">
          <v-btn icon="mdi-content-copy" variant="text" size="x-small" class="mr-1" @click="copyToClipboard(String(value))" />
          <span class="text-mono">{{ value }}</span>
        </template>
        <template #item.QueryTrunc="{ value }">
          <code class="sql-highlight text-mono text-body-2" v-html="highlightSql(value)"></code>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
