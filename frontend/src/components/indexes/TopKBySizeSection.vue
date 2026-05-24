<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIndexesTopKBySize } from '@/api/gen/default/default'
import type { IndexTopKBySize } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import { useDescribeLink } from '@/composables/useDescribeLink'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { describeLinkFromQualified } = useDescribeLink()
const { t } = useI18n()
const { onError } = useViewError()

const headers = computed(() => [
  { title: t('header.tablespace'), key: 'Tablespace' },
  { title: t('header.table'), key: 'Table' },
  { title: t('header.index'), key: 'Index' },
  { title: t('header.size'), key: 'SizeBytes' },
])

const { items, loading } = useApiLoader<IndexTopKBySize[]>(
  () => getIndexesTopKBySize({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
  }),
  {
    deps: [clusterName, hostName, databaseName],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError,
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title><v-icon start icon="mdi-sort-descending" />{{ t('indexes.topKBySize') }}</v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading">
        <template #item.Table="{ item }">
          <router-link :to="describeLinkFromQualified(item.Table)" class="text-decoration-none">{{ item.Table }}</router-link>
        </template>
        <template #item.SizeBytes="{ item }">{{ item.Size }}</template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
