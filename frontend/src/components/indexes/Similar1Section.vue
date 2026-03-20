<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIndexesSimilar1 } from '@/api/gen/default/default'
import type { IndexSimilar1 } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.table'), key: 'Table' },
  { title: t('header.uniqueIndexName'), key: 'I1UniqueIndexName' },
  { title: t('header.indexName2'), key: 'I2IndexName' },
  { title: t('header.uniqueIndexDef'), key: 'I1UniqueIndexDefinition' },
  { title: t('header.indexDef2'), key: 'I2IndexDefinition' },
  { title: t('header.usedInConstraint1'), key: 'I1UsedInConstraint' },
  { title: t('header.usedInConstraint2'), key: 'I2UsedInConstraint' },
])

const { items, loading } = useApiLoader<IndexSimilar1[]>(
  () => getIndexesSimilar1({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
  }),
  {
    deps: [clusterName, hostName, databaseName],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError: (msg) => emit('error', msg),
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('indexes.similar1') }}</v-card-title>
    <v-card-subtitle class="text-wrap">{{ t('indexes.similar1Hint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" />
    </v-card-text>
  </v-card>
</template>
