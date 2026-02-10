<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import hljs from 'highlight.js/lib/core'
import pgsql from 'highlight.js/lib/languages/pgsql'
import { getQueriesTop10ByTime } from '@/api/gen/default/default'
import type { QueryTop10ByTime } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

hljs.registerLanguage('pgsql', pgsql)

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.queryId'), key: 'QueryID' },
  { title: t('header.execTime'), key: 'ExecTimeMs' },
  { title: t('header.ioCpuPct'), key: 'IoCpuPct', sortable: false },
  { title: t('header.queryTrunc'), key: 'QueryTrunc' },
])
const items = ref<QueryTop10ByTime[]>([])
const loading = ref(false)

function highlightSql(sql: string): string {
  return hljs.highlight(sql, { language: 'pgsql' }).value
}

function copyToClipboard(text: string) {
  if (navigator.clipboard) {
    navigator.clipboard.writeText(text)
  } else {
    const ta = document.createElement('textarea')
    ta.value = text
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
}

async function load() {
  if (!clusterName.value || !hostName.value) return
  loading.value = true
  try {
    const response = await getQueriesTop10ByTime({
      cluster_name: clusterName.value,
      instance: hostName.value,
    })
    items.value = assertOk(response) ?? []
  } catch (err) {
    emit('error', String(err))
    items.value = []
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName], () => load(), { immediate: true })
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
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort disable-pagination hide-default-footer>
        <template #item.ExecTimeMs="{ item }">{{ item.ExecTime }}</template>
        <template #item.QueryID="{ value }">
          <v-btn icon="mdi-content-copy" variant="text" size="x-small" class="mr-1" @click="copyToClipboard(String(value))" />
          <span style="font-family: monospace;">{{ value }}</span>
        </template>
        <template #item.QueryTrunc="{ value }">
          <code class="sql-highlight text-body-2" style="font-family: monospace;" v-html="highlightSql(value)"></code>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.sql-highlight :deep(.hljs-keyword) { color: #cf222e; }
.sql-highlight :deep(.hljs-string) { color: #0a3069; }
.sql-highlight :deep(.hljs-number) { color: #0550ae; }
.sql-highlight :deep(.hljs-built_in) { color: #8250df; }
.sql-highlight :deep(.hljs-type) { color: #8250df; }
.sql-highlight :deep(.hljs-comment) { color: #6e7781; }
.sql-highlight :deep(.hljs-operator) { color: #cf222e; }

.v-theme--dark .sql-highlight :deep(.hljs-keyword) { color: #ff7b72; }
.v-theme--dark .sql-highlight :deep(.hljs-string) { color: #a5d6ff; }
.v-theme--dark .sql-highlight :deep(.hljs-number) { color: #79c0ff; }
.v-theme--dark .sql-highlight :deep(.hljs-built_in) { color: #d2a8ff; }
.v-theme--dark .sql-highlight :deep(.hljs-type) { color: #d2a8ff; }
.v-theme--dark .sql-highlight :deep(.hljs-comment) { color: #8b949e; }
.v-theme--dark .sql-highlight :deep(.hljs-operator) { color: #ff7b72; }
</style>
