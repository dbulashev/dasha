<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { getMaintenanceAutovacuumFreezeMaxAge } from '@/api/gen/default/default'
import type { MaintenanceAutovacuumFreezeMaxAge } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const { items, loading } = useApiLoader<MaintenanceAutovacuumFreezeMaxAge[]>(
  () => getMaintenanceAutovacuumFreezeMaxAge({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: (msg) => emit('error', msg),
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title><v-icon start icon="mdi-snowflake-alert" />{{ t('maintenance.autovacuumFreezeMaxAge') }}</v-card-title>
    <v-card-text>
      <v-progress-linear v-if="loading" indeterminate />
      <div v-else-if="items.length" class="d-flex flex-wrap ga-2">
        <v-chip v-for="(item, idx) in items" :key="idx" size="large" variant="tonal">
          autovacuum_freeze_max_age = {{ item.AutovacuumFreezeMaxAge }}
        </v-chip>
      </div>
      <div v-else class="text-medium-emphasis">{{ t('noData') }}</div>
    </v-card-text>
  </v-card>
</template>
