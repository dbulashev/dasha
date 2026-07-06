<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getLogs } from '@/api/gen/default/default'
import type { GetLogsParams, LogEntry, LogSearchResult } from '@/api/models'
import { ApiError, assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { useClusterInfo } from '@/composables/useClusterInfo'
import LogFilterBar from './LogFilterBar.vue'
import LogResultsTable from './LogResultsTable.vue'
import type { LogFilters } from './types'

const { t } = useI18n()
const { clusterName, currentCluster } = useClusterInfo()

const items = ref<LogEntry[]>([])
const loading = ref(false)
const dedup = ref(false)
const partial = ref(false)
const scanned = ref(0)
const nextToken = ref<string | undefined>(undefined)
const errorMsg = ref<string>('')
const searched = ref(false)

const lastFilters = ref<LogFilters | null>(null)

const hosts = computed(() =>
  (currentCluster.value?.instances ?? [])
    .map(i => i.host_name)
    .filter((h): h is string => !!h),
)

const hasMore = computed(() => !dedup.value && !!nextToken.value)

const activeMessage = computed(() => lastFilters.value?.message ?? '')

function mapError(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.status) {
      case 400:
        return t('logs.error.badRequest')
      case 404:
        return t('logs.error.notFound')
      case 501:
        return t('logs.error.unsupported')
      case 502:
        return t('logs.error.upstream')
      case 504:
        return t('logs.error.timeout')
    }
  }
  return getErrorMessage(err)
}

async function runSearch(filters: LogFilters, append: boolean) {
  if (!clusterName.value) return

  loading.value = true
  errorMsg.value = ''

  if (!append) {
    items.value = []
    nextToken.value = undefined
  }

  try {
    const params: GetLogsParams = {
      cluster_name: clusterName.value,
      service_type: filters.serviceType,
      from: filters.from,
      to: filters.to,
      severity: filters.severities.length ? filters.severities : undefined,
      host: filters.host || undefined,
      message: filters.message || undefined,
      database: filters.database || undefined,
      user: filters.user || undefined,
      dedup: filters.dedup,
      page_size: filters.pageSize,
      page_token: append ? nextToken.value : undefined,
    }

    const res = assertOk(await getLogs(params)) as LogSearchResult

    items.value = append ? [...items.value, ...res.items] : res.items
    dedup.value = res.dedup
    partial.value = res.partial
    scanned.value = res.scanned ?? 0
    nextToken.value = res.next_page_token
    lastFilters.value = filters
    searched.value = true
  } catch (err) {
    errorMsg.value = mapError(err)
    if (!append) {
      // A fresh search failed: drop the previous search's result state so
      // stale "no results" / "scan limit" alerts don't render next to the error.
      searched.value = false
      dedup.value = false
      partial.value = false
      scanned.value = 0
    }
  } finally {
    loading.value = false
  }
}

function onSearch(filters: LogFilters) {
  runSearch(filters, false)
}

function onLoadMore() {
  if (lastFilters.value) runSearch(lastFilters.value, true)
}
</script>

<template>
  <LogFilterBar :hosts="hosts" :loading="loading" @search="onSearch" />

  <v-alert
    v-if="errorMsg"
    type="error"
    variant="tonal"
    class="mb-4"
    closable
    @click:close="errorMsg = ''"
  >
    {{ errorMsg }}
  </v-alert>

  <LogResultsTable
    :items="items"
    :loading="loading"
    :dedup="dedup"
    :partial="partial"
    :scanned="scanned"
    :has-more="hasMore"
    :message="activeMessage"
    :searched="searched"
    @load-more="onLoadMore"
  />
</template>
