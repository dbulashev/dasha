<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIndexesInvalidOrNotReady } from '@/api/gen/default/default'
import type { IndexInvalidOrNotReady } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.table'), key: 'Table' },
  { title: t('header.indexName'), key: 'IndexName' },
  { title: t('header.isValid'), key: 'IsValid' },
  { title: t('header.isReady'), key: 'IsReady' },
  { title: t('header.constraint'), key: 'Constraint' },
])

const { items, loading } = useApiLoader<IndexInvalidOrNotReady[]>(
  () => getIndexesInvalidOrNotReady({
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
    <v-card-title>{{ t('indexes.invalidOrNotReady') }}</v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" />
    </v-card-text>
  </v-card>
</template>
