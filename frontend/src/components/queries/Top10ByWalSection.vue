<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQueriesTop10ByWal } from '@/api/gen/default/default'
import type { QueryTop10ByWal } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import { highlightSql, copyToClipboard } from '@/utils/sql'
import '@/assets/sql-highlight.css'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.queryId'), key: 'QueryID' },
  { title: t('header.walVolume'), key: 'WalBytes' },
  { title: t('header.queryTrunc'), key: 'QueryTrunc' },
])
const items = ref<QueryTop10ByWal[]>([])
const loading = ref(false)

async function load() {
  if (!clusterName.value || !hostName.value) return
  loading.value = true
  try {
    const response = await getQueriesTop10ByWal({
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
      {{ t('Top 10 by WAL Volume') }}
      <v-tooltip :text="t('hint.queryStats')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer>
        <template #item.WalBytes="{ item }">{{ item.WalVolume }}</template>
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

