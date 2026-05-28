<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { getHealthScore } from '@/api/gen/default/default'
import type { HealthScore } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import HealthScoreGauge from '@/components/health-score/HealthScoreGauge.vue'
import HealthScoreCategories from '@/components/health-score/HealthScoreCategories.vue'
import HealthScoreDatabases from '@/components/health-score/HealthScoreDatabases.vue'
import HealthScoreRecommendations from '@/components/health-score/HealthScoreRecommendations.vue'
import HealthScoreAbout from '@/components/health-score/HealthScoreAbout.vue'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()
const route = useRoute()
const router = useRouter()

const selectedDatabase = computed<string | null>(() => {
  const q = route.query.database
  return typeof q === 'string' && q.length > 0 ? q : null
})

function clearDatabase() {
  const { database, ...rest } = route.query
  void database
  router.replace({ name: 'HealthScore', params: route.params, query: rest })
}

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
  <div>
    <h1 class="text-h5 mb-4 d-flex align-center ga-2">
      <v-icon icon="mdi-heart-pulse" />
      {{ t('healthScore.page.title') }}
    </h1>

    <v-card class="mb-4">
      <v-card-text>
        <v-skeleton-loader v-if="loading" type="heading, text@3" />
        <div v-else-if="data" class="d-flex align-center ga-8 flex-wrap">
          <HealthScoreGauge :score="data.score" :size="160" :width="14" />
          <div class="flex-grow-1" style="min-width: 280px">
            <HealthScoreCategories :categories="data.categories" />
          </div>
        </div>
      </v-card-text>
    </v-card>

    <HealthScoreDatabases class="mb-4" />

    <HealthScoreRecommendations
      class="mb-4"
      :database="selectedDatabase"
      @clear-database="clearDatabase"
    />

    <HealthScoreAbout />
  </div>
</template>
