<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { getDatabaseHealth } from '@/api/gen/default/default'
import type { DatabaseHealth } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()

const { items: health, loading } = useApiLoader<DatabaseHealth>(
  () => getDatabaseHealth({
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

const ROLLBACK_THRESHOLD = 0.05
</script>

<template>
  <v-card class="h-100">
    <v-card-title class="d-flex align-center">
      <v-icon start icon="mdi-heart-pulse" />
      {{ t('home.databaseHealth') }}
    </v-card-title>
    <v-card-text>
      <v-skeleton-loader v-if="loading" type="chip@4" />
      <div v-else-if="health" class="d-flex flex-wrap ga-3">
        <v-tooltip :text="t('home.hint.deadlocks')" location="bottom" max-width="400">
          <template #activator="{ props: tp }">
            <v-chip
              v-bind="tp"
              :color="health.Deadlocks > 0 ? 'warning' : 'success'"
              variant="tonal"
              :prepend-icon="health.Deadlocks > 0 ? 'mdi-alert' : 'mdi-check-circle'"
            >
              {{ t('home.deadlocks') }}: {{ health.Deadlocks }}
            </v-chip>
          </template>
        </v-tooltip>

        <v-tooltip v-if="health.ChecksumFailures != null" :text="t('home.hint.checksumFailures')" location="bottom" max-width="400">
          <template #activator="{ props: tp }">
            <v-chip
              v-bind="tp"
              :color="health.ChecksumFailures > 0 ? 'error' : 'success'"
              variant="tonal"
              :prepend-icon="health.ChecksumFailures > 0 ? 'mdi-alert-octagon' : 'mdi-check-circle'"
            >
              {{ t('home.checksumFailures') }}: {{ health.ChecksumFailures }}
            </v-chip>
          </template>
        </v-tooltip>

        <v-tooltip :text="t('home.hint.conflicts')" location="bottom" max-width="400">
          <template #activator="{ props: tp }">
            <v-chip
              v-bind="tp"
              :color="health.Conflicts > 0 ? 'warning' : 'success'"
              variant="tonal"
              :prepend-icon="health.Conflicts > 0 ? 'mdi-alert' : 'mdi-check-circle'"
            >
              {{ t('home.conflicts') }}: {{ health.Conflicts }}
            </v-chip>
          </template>
        </v-tooltip>

        <v-tooltip :text="t('home.hint.rollbackRatio')" location="bottom" max-width="400">
          <template #activator="{ props: tp }">
            <v-chip
              v-bind="tp"
              :color="health.RollbackRatio > ROLLBACK_THRESHOLD ? 'warning' : 'success'"
              variant="tonal"
              :prepend-icon="health.RollbackRatio > ROLLBACK_THRESHOLD ? 'mdi-alert' : 'mdi-check-circle'"
            >
              {{ t('home.rollbackRatio') }}: {{ (health.RollbackRatio * 100).toFixed(2) }}%
            </v-chip>
          </template>
        </v-tooltip>

        <v-tooltip v-if="health.StatsReset" :text="t('home.hint.statsSince')" location="bottom" max-width="400">
          <template #activator="{ props: tp }">
            <v-chip v-bind="tp" size="small" variant="text" prepend-icon="mdi-clock-outline">
              {{ t('home.statsSince') }} {{ new Date(health.StatsReset).toLocaleDateString() }}
            </v-chip>
          </template>
        </v-tooltip>
      </div>
    </v-card-text>
  </v-card>
</template>
