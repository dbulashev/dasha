<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIndexesSimilar2 } from '@/api/gen/default/default'
import type { IndexSimilar2 } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.table'), key: 'Table' },
  { title: t('header.fkName'), key: 'FkName' },
  { title: t('header.fkName2'), key: 'FkName2' },
])

const { items, loading } = useApiLoader<IndexSimilar2[]>(
  () => getIndexesSimilar2({
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
    <v-card-title>{{ t('indexes.similar2') }}</v-card-title>
    <v-card-subtitle class="text-wrap">{{ t('indexes.similar2Hint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" />
    </v-card-text>
  </v-card>
</template>
