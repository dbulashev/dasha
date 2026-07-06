<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { getDatabaseHealth, getHealthScore } from '@/api/gen/default/default'
import type { DatabaseHealth, HealthScore } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import HealthScoreGauge from '@/components/health-score/HealthScoreGauge.vue'
import HealthScoreCategories from '@/components/health-score/HealthScoreCategories.vue'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()
const route = useRoute()

const detailsLink = computed(() => ({
  name: 'HealthScore',
  params: { clustername: route.params.clustername ?? '' },
  query: route.query,
}))

const { items: data, loading } = useApiLoader<HealthScore | null>(
  () =>
    getHealthScore({
      cluster_name: clusterName.value!,
      instance: hostName.value!,
    }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError,
    defaultValue: null,
  },
)

// Per-database health chips merged from the former DatabaseHealthSection card.
// These signals (checksum failures, recovery conflicts, rollback ratio) are
// not covered by HealthScore rules and complement the aggregate score.
const { items: dbHealth } = useApiLoader<DatabaseHealth | null>(
  () =>
    getDatabaseHealth({
      cluster_name: clusterName.value!,
      instance: hostName.value!,
      database: databaseName.value!,
    }),
  {
    deps: [clusterName, hostName, databaseName],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError,
    defaultValue: null,
  },
)

const ROLLBACK_THRESHOLD = 0.05
</script>

<template>
  <v-card>
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-heart-pulse" />
      {{ t('healthScore.title') }}
      <v-tooltip :text="t('healthScore.tooltip')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
      <v-spacer />
      <v-btn
        :to="detailsLink"
        variant="text"
        size="small"
        append-icon="mdi-arrow-right"
      >
        {{ t('healthScore.openDetailsPage') }}
      </v-btn>
    </v-card-title>
    <v-card-text>
      <v-skeleton-loader v-if="loading" type="heading, text@3" />
      <template v-else-if="data">
        <v-alert
          v-if="data.metrics_degraded"
          type="warning"
          variant="tonal"
          density="compact"
          class="mb-4"
          icon="mdi-alert"
          :text="t('healthScore.metricsDegraded')"
        />
        <div class="d-flex align-center ga-6 mb-4">
          <HealthScoreGauge :score="data.score" :size="100" />
          <div class="flex-grow-1">
            <HealthScoreCategories :categories="data.categories" />
          </div>
        </div>

        <v-divider v-if="dbHealth" class="mb-3" />
        <div v-if="dbHealth" class="d-flex flex-wrap ga-2 align-center">
          <v-tooltip
            v-if="dbHealth.ChecksumFailures != null"
            :text="t('home.hint.checksumFailures')"
            location="bottom"
            max-width="400"
          >
            <template #activator="{ props: tp }">
              <v-chip
                v-bind="tp"
                size="small"
                :color="dbHealth.ChecksumFailures > 0 ? 'error' : 'success'"
                variant="tonal"
                :prepend-icon="dbHealth.ChecksumFailures > 0 ? 'mdi-alert-octagon' : 'mdi-check-circle'"
              >
                {{ t('home.checksumFailures') }}: {{ dbHealth.ChecksumFailures }}
              </v-chip>
            </template>
          </v-tooltip>

          <v-tooltip :text="t('home.hint.conflicts')" location="bottom" max-width="400">
            <template #activator="{ props: tp }">
              <v-chip
                v-bind="tp"
                size="small"
                :color="dbHealth.Conflicts > 0 ? 'warning' : 'success'"
                variant="tonal"
                :prepend-icon="dbHealth.Conflicts > 0 ? 'mdi-alert' : 'mdi-check-circle'"
              >
                {{ t('home.conflicts') }}: {{ dbHealth.Conflicts }}
              </v-chip>
            </template>
          </v-tooltip>

          <v-tooltip :text="t('home.hint.rollbackRatio')" location="bottom" max-width="400">
            <template #activator="{ props: tp }">
              <v-chip
                v-bind="tp"
                size="small"
                :color="dbHealth.RollbackRatio > ROLLBACK_THRESHOLD ? 'warning' : 'success'"
                variant="tonal"
                :prepend-icon="dbHealth.RollbackRatio > ROLLBACK_THRESHOLD ? 'mdi-alert' : 'mdi-check-circle'"
              >
                {{ t('home.rollbackRatio') }}: {{ (dbHealth.RollbackRatio * 100).toFixed(2) }}%
              </v-chip>
            </template>
          </v-tooltip>

          <v-tooltip
            v-if="dbHealth.StatsReset"
            :text="t('home.hint.statsSince')"
            location="bottom"
            max-width="400"
          >
            <template #activator="{ props: tp }">
              <v-chip v-bind="tp" size="x-small" variant="text" prepend-icon="mdi-clock-outline">
                {{ t('home.statsSince') }} {{ new Date(dbHealth.StatsReset).toLocaleDateString() }}
              </v-chip>
            </template>
          </v-tooltip>
        </div>
      </template>
    </v-card-text>
  </v-card>
</template>
