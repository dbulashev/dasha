<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getInvalidConstraints } from '@/api/gen/default/default'
import type { InvalidConstraint } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.schema'), key: 'Schema' },
  { title: t('header.table'), key: 'Table' },
  { title: t('header.constraint'), key: 'Name' },
  { title: t('fk.referencedSchema'), key: 'ReferencedSchema' },
  { title: t('fk.referencedTable'), key: 'ReferencedTable' },
])

const { items, loading } = useApiLoader<InvalidConstraint[]>(
  () => getInvalidConstraints({
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
    <v-card-title>{{ t('fk.invalidConstraints') }}</v-card-title>
    <v-card-subtitle>{{ t('fk.invalidConstraintsHint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer :no-data-text="t('fk.noInvalidConstraints')" />
    </v-card-text>
  </v-card>
</template>
