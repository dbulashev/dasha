<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { getHealthScore } from '@/api/gen/default/default'
import type { HealthScore } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import HealthScoreGauge from '@/components/health-score/HealthScoreGauge.vue'
import HealthScoreCategories from '@/components/health-score/HealthScoreCategories.vue'

const { clusterName, hostName } = useClusterInfo()
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
        <div class="d-flex align-center ga-6 mb-4">
          <HealthScoreGauge :score="data.score" :size="100" />
          <div class="flex-grow-1">
            <HealthScoreCategories :categories="data.categories" />
          </div>
        </div>
      </template>
    </v-card-text>
  </v-card>
</template>
