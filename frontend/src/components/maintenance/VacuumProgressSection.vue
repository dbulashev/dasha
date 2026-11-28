<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getMaintenanceVacuumProgress } from '@/api/gen/default/default'
import type { MaintenanceVacuumProgress } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import { modeColor, startedByColor, vacuumWarning } from '@/components/progress/pg19'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()

const headers = computed(() => [
  { title: 'PID', key: 'Pid' },
  { title: t('progress.phase'), key: 'Phase' },
  ...(hasPg19Columns.value
    ? [
        { title: t('progress.startedByTitle'), key: 'StartedBy' },
        { title: t('progress.modeTitle'), key: 'Mode' },
      ]
    : []),
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
    onError,
  },
)

const hasPg19Columns = computed(() => items.value.some(i => i.StartedBy != null || i.Mode != null))

const warnings = computed(() => {
  const kinds = new Set(items.value.map(i => vacuumWarning(i.StartedBy, i.Mode)).filter(w => w !== null))
  return [...kinds]
})
</script>

<template>
  <v-card class="mb-4">
    <v-card-title><v-icon start icon="mdi-broom" />{{ t('maintenance.vacuumProgress') }}</v-card-title>
    <v-card-text>
      <v-alert
        v-for="w in warnings"
        :key="w"
        type="warning"
        variant="tonal"
        density="compact"
        class="mb-2"
      >
        {{ t(`progress.vacuumWarning.${w}`) }}
      </v-alert>
      <v-data-table :headers="headers" :items="items" :loading="loading" item-value="Pid">
        <template #item.Phase="{ item }">{{ item.Phase || '—' }}</template>
        <template #item.StartedBy="{ item }">
          <v-chip v-if="item.StartedBy" size="small" variant="tonal" :color="startedByColor(item.StartedBy)">
            {{ t(`progress.startedBy.${item.StartedBy}`, item.StartedBy) }}
          </v-chip>
        </template>
        <template #item.Mode="{ item }">
          <v-chip v-if="item.Mode" size="small" variant="tonal" :color="modeColor(item.Mode)">
            {{ t(`progress.mode.${item.Mode}`, item.Mode) }}
          </v-chip>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
