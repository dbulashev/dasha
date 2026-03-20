<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIndexesBloat } from '@/api/gen/default/default'
import type { IndexBloat } from '@/api/models/index'
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
  { title: t('header.index'), key: 'Index' },
  { title: t('header.bloatBytes'), key: 'BloatBytes' },
  { title: t('header.indexBytes'), key: 'IndexBytes' },
  { title: t('header.primary'), key: 'Primary' },
])

const { items, loading, page, hasMore, load } = usePaginatedApiLoader<IndexBloat>(
  (limit, offset) => getIndexesBloat({
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
      {{ t('indexes.bloat') }}
      <v-tooltip :text="t('hint.indexBloat')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer />
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
