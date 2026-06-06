<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getHealthScoreRecommendations } from '@/api/gen/default/default'
import type { HealthScoreRecommendations } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import HealthScoreRecommendationCard from './HealthScoreRecommendation.vue'

const props = defineProps<{
  database?: string | null
}>()

const emit = defineEmits<{
  'clear-database': []
}>()

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()

const { items: data, loading, load } = useApiLoader<HealthScoreRecommendations | null>(
  () =>
    getHealthScoreRecommendations({
      cluster_name: clusterName.value!,
      instance: hostName.value!,
      ...(props.database ? { database: props.database } : {}),
    }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError,
    defaultValue: null,
  },
)

// Reload when drill-down database changes.
watch(() => props.database, () => load())

const recs = computed(() => data.value?.recommendations ?? [])
</script>

<template>
  <v-card>
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <v-icon start icon="mdi-clipboard-list-outline" />
      <span>{{ t('healthScore.recommendations.title') }}</span>
      <v-chip
        v-if="database"
        color="primary"
        variant="tonal"
        size="small"
        closable
        prepend-icon="mdi-database"
        @click:close="emit('clear-database')"
      >
        {{ database }}
      </v-chip>
    </v-card-title>
    <v-card-text>
      <v-skeleton-loader v-if="loading" type="paragraph@2" />
      <template v-else>
        <v-alert
          v-if="recs.length === 0"
          type="success"
          variant="tonal"
          density="compact"
          icon="mdi-check-circle"
        >
          {{ t('healthScore.page.noRecommendations') }}
        </v-alert>
        <HealthScoreRecommendationCard
          v-for="r in recs"
          :key="r.rule_id"
          :rec="r"
          :database="database ?? null"
        />
      </template>
    </v-card-text>
  </v-card>
</template>
