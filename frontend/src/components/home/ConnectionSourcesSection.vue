<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getConnectionSources } from '@/api/gen/default/default'
import type { ConnectionSource } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { usePaginatedApiLoader } from '@/composables/useApiLoader'
import { LARGE_PAGE_SIZE } from '@/constants/pagination'
import PaginationControls from '@/components/PaginationControls.vue'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.database'), key: 'Database' },
  { title: t('header.user'), key: 'Username' },
  { title: t('home.applicationName'), key: 'ApplicationName' },
  { title: t('home.clientAddr'), key: 'ClientAddr' },
  { title: t('header.amount'), key: 'TotalConnections' },
])

const { items, loading, page, hasMore, load } = usePaginatedApiLoader<ConnectionSource>(
  (limit, offset) => getConnectionSources({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    limit,
    offset,
  }),
  {
    pageSize: LARGE_PAGE_SIZE,
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: (msg) => emit('error', msg),
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('home.connectionSources') }}</v-card-title>
    <v-card-text>
      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
        density="compact"
        multi-sort
        :items-per-page="-1"
        hide-default-footer
        :no-data-text="t('noData')"
      />
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
