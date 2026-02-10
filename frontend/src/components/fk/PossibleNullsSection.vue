<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getFksPossibleNulls } from '@/api/gen/default/default'
import type { FksPossibleNulls } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('fk.fkName'), key: 'FkName' },
  { title: t('header.table'), key: 'RelName' },
  { title: t('fk.columns'), key: 'AttNames' },
])

const { items, loading } = useApiLoader<FksPossibleNulls[]>(
  () => getFksPossibleNulls({
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
    <v-card-title><v-icon start icon="mdi-null" />{{ t('fk.possibleNulls') }}</v-card-title>
    <v-card-subtitle>{{ t('fk.possibleNullsHint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading">
        <template #item.AttNames="{ value }">{{ (value as string[]).join(', ') }}</template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
