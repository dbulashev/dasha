<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getMaintenanceVacuumProgress } from '@/api/gen/default/default'
import type { MaintenanceVacuumProgress } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: 'PID', key: 'Pid' },
  { title: t('progress.phase'), key: 'Phase' },
])

const { items, loading } = useApiLoader<MaintenanceVacuumProgress[]>(
  () => getMaintenanceVacuumProgress({
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
    <v-card-title><v-icon start icon="mdi-broom" />{{ t('maintenance.vacuumProgress') }}</v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" />
    </v-card-text>
  </v-card>
</template>
