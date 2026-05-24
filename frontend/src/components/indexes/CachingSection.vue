<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useViewError } from '@/composables/useViewError'
import { getIndexesCaching } from '@/api/gen/default/default'
import type { IndexCaching } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { usePaginatedApiLoader } from '@/composables/useApiLoader'
import { DEFAULT_PAGE_SIZE } from '@/constants/pagination'
import PaginationControls from '@/components/PaginationControls.vue'
import { useDescribeLink } from '@/composables/useDescribeLink'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { describeLink } = useDescribeLink()
const { t } = useI18n()
const { onError } = useViewError()

const headers = computed(() => [
  { title: t('header.schema'), key: 'Schema', sortable: false },
  { title: t('header.table'), key: 'Table', sortable: false },
  { title: t('header.index'), key: 'Index', sortable: false },
  { title: t('header.hitRate'), key: 'HitRate', sortable: false },
])

const { items, loading, page, hasMore, load } = usePaginatedApiLoader<IndexCaching>(
  (limit, offset) => getIndexesCaching({
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
    onError,
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-cached" />{{ t('indexes.caching') }}
      <v-tooltip :text="t('hint.indexCaching')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading">
        <template #item.Table="{ item }">
          <router-link :to="describeLink(item.Schema, item.Table)" class="text-decoration-none">{{ item.Table }}</router-link>
        </template>
        <template #item.HitRate="{ value }">
          {{ value != null ? (value * 100).toFixed(2) + '%' : '—' }}
        </template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
