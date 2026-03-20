<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getTablesCaching } from '@/api/gen/default/default'
import type { TableCaching } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { usePaginatedApiLoader } from '@/composables/useApiLoader'
import { DEFAULT_PAGE_SIZE } from '@/constants/pagination'
import { fmtPct } from '@/utils/format'
import PaginationControls from '@/components/PaginationControls.vue'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.schema'), key: 'Schema' },
  { title: t('header.table'), key: 'Table' },
  { title: t('tables.heapHitRate'), key: 'HitRate' },
  { title: t('tables.idxHitRate'), key: 'IdxHitRate' },
  { title: t('tables.toastHitRate'), key: 'ToastHitRate' },
  { title: t('tables.toastIdxHitRate'), key: 'ToastIdxHitRate' },
])

const { items, loading, page, hasMore, load } = usePaginatedApiLoader<TableCaching>(
  (limit, offset) => getTablesCaching({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
    limit,
    offset,
  }),
  {
    pageSize: DEFAULT_PAGE_SIZE,
    deps: [clusterName, hostName, databaseName],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError: (msg) => emit('error', msg),
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      {{ t('tables.caching') }}
      <v-tooltip :text="t('hint.tableCaching')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading">
        <template #item.HitRate="{ value }">{{ fmtPct(value, 2) }}</template>
        <template #item.IdxHitRate="{ value }">{{ fmtPct(value, 2) }}</template>
        <template #item.ToastHitRate="{ value }">{{ fmtPct(value, 2) }}</template>
        <template #item.ToastIdxHitRate="{ value }">{{ fmtPct(value, 2) }}</template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
