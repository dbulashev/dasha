<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIndexesUnused } from '@/api/gen/default/default'
import type { IndexUnused } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { usePaginatedApiLoader } from '@/composables/useApiLoader'
import { DEFAULT_PAGE_SIZE } from '@/constants/pagination'
import PaginationControls from '@/components/PaginationControls.vue'
import { fmtBytes } from '@/utils/format'
import { useDescribeLink } from '@/composables/useDescribeLink'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { describeLink } = useDescribeLink()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const thresholdOptions = [0, 100, 1000, 10000]
const headers = computed(() => [
  { title: t('header.schema'), key: 'Schema' },
  { title: t('header.table'), key: 'Table' },
  { title: t('header.index'), key: 'Index' },
  { title: t('header.size'), key: 'SizeBytes' },
  { title: t('header.indexScans'), key: 'IndexScans' },
])

const allHosts = ref(false)
const threshold = ref(0)

const { items, loading, page, hasMore, load } = usePaginatedApiLoader<IndexUnused>(
  (limit, offset) => getIndexesUnused({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
    limit,
    offset,
    all_hosts: allHosts.value || undefined,
    threshold: threshold.value || undefined,
  }),
  {
    pageSize: DEFAULT_PAGE_SIZE,
    deps: [clusterName, hostName, databaseName, allHosts, threshold],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError: (msg) => emit('error', msg),
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center">
      <v-icon start icon="mdi-archive-outline" />{{ t('indexes.unused') }}
      <v-spacer />
      <v-checkbox
        v-model="allHosts"
        :label="t('indexes.allHostsCheck')"
        density="compact"
        hide-details
        class="mr-4"
      />
      <v-select
        v-model="threshold"
        :items="thresholdOptions"
        :label="t('indexes.thresholdLabel')"
        density="compact"
        hide-details
        style="max-width: 200px"
      />
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading">
        <template #item.Table="{ item }">
          <router-link :to="describeLink(item.Schema, item.Table)" class="text-decoration-none">{{ item.Table }}</router-link>
        </template>
        <template #item.SizeBytes="{ value }">{{ fmtBytes(value) }}</template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
