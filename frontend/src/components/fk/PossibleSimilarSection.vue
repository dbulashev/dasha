<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getFksPossibleSimilar } from '@/api/gen/default/default'
import type { FksPossibleSimilar } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.table'), key: 'Table' },
  { title: t('fk.fkName'), key: 'FkName' },
  { title: t('fk.similarFk'), key: 'Fk1Name' },
])

const { items, loading } = useApiLoader<FksPossibleSimilar[]>(
  () => getFksPossibleSimilar({
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
    <v-card-title>{{ t('fk.possibleSimilar') }}</v-card-title>
    <v-card-subtitle>{{ t('fk.possibleSimilarHint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer :no-data-text="t('noData')" />
    </v-card-text>
  </v-card>
</template>
