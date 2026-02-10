<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getFkTypeMismatch } from '@/api/gen/default/default'
import type { FkTypeMismatch } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('fk.fkName'), key: 'FkName' },
  { title: t('fk.fromTable'), key: 'FromRel' },
  { title: t('fk.columns'), key: 'RelAttNames' },
  { title: t('fk.toTable'), key: 'ToRel' },
  { title: t('fk.toColumns'), key: 'ToRelAttNames' },
])

const { items, loading } = useApiLoader<FkTypeMismatch[]>(
  () => getFkTypeMismatch({
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
    <v-card-title><v-icon start icon="mdi-swap-horizontal" />{{ t('fk.typeMismatch') }}</v-card-title>
    <v-card-subtitle>{{ t('fk.typeMismatchHint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading">
        <template #item.RelAttNames="{ value }">{{ (value as string[]).join(', ') }}</template>
        <template #item.ToRelAttNames="{ value }">{{ (value as string[]).join(', ') }}</template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
