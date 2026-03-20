<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIndexesUsage } from '@/api/gen/default/default'
import type { IndexUsage } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { usePaginatedApiLoader } from '@/composables/useApiLoader'
import { DEFAULT_PAGE_SIZE } from '@/constants/pagination'
import PaginationControls from '@/components/PaginationControls.vue'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.schema'), key: 'Schema' },
  { title: t('header.table'), key: 'Table' },
  { title: t('header.percentIndexUsed'), key: 'PercentOfTimesIndexUsed' },
  { title: t('header.estimatedRows'), key: 'EstimatedRows' },
])

const { items, loading, page, hasMore, load } = usePaginatedApiLoader<IndexUsage>(
  (limit, offset) => getIndexesUsage({
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
      {{ t('indexes.usage') }}
      <v-tooltip :text="t('hint.indexUsage')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer>
        <template #item.PercentOfTimesIndexUsed="{ value }">{{ value != null ? value + '%' : t('insufficientData') }}</template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
