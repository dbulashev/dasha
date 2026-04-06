<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getHealthScore } from '@/api/gen/default/default'
import type { HealthScore, HealthScoreCategory } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const { items: data, loading } = useApiLoader<HealthScore | null>(
  () =>
    getHealthScore({
      cluster_name: clusterName.value!,
      instance: hostName.value!,
    }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: (msg) => emit('error', msg),
    defaultValue: null,
  },
)

function scoreColor(score: number): string {
  if (score >= 95) return 'success'
  if (score >= 70) return 'warning'
  if (score >= 40) return 'orange'
  return 'error'
}

function scoreLabel(score: number): string {
  if (score >= 95) return t('healthScore.excellent')
  if (score >= 70) return t('healthScore.warning')
  if (score >= 40) return t('healthScore.problems')
  return t('healthScore.critical')
}

const visibleCategories = computed(() => {
  if (!data.value) return []
  return data.value.categories.filter((c: HealthScoreCategory) => c.weight > 0)
})

function categoryLabel(name: string): string {
  return t(`healthScore.categories.${name}`)
}

function categoryIcon(name: string): string {
  const icons: Record<string, string> = {
    connections: 'mdi-lan-connect',
    performance: 'mdi-speedometer',
    storage: 'mdi-database',
    replication: 'mdi-content-copy',
    maintenance: 'mdi-wrench',
  }
  return icons[name] ?? 'mdi-circle'
}

function formatDetail(key: string, value: number): string {
  if (key === 'cache_hit_ratio') return value.toFixed(2) + '%'
  if (key.endsWith('_ratio')) return value.toFixed(3)
  if (key === 'max_lag_bytes') return formatBytes(value)
  if (key.endsWith('_seconds') || key.endsWith('_hours')) return value.toFixed(1)
  return String(value)
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

function detailLabel(key: string): string {
  return t(`healthScore.details.${key}`, key)
}
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
    </v-card-title>
    <v-card-text>
      <v-skeleton-loader v-if="loading" type="heading, text@3" />
      <template v-else-if="data">
        <div class="d-flex align-center ga-6 mb-4">
          <v-progress-circular
            :model-value="data.score"
            :color="scoreColor(data.score)"
            :size="100"
            :width="10"
          >
            <div class="text-center">
              <div class="text-h5 font-weight-bold">{{ Math.round(data.score) }}</div>
              <div class="text-caption">{{ scoreLabel(data.score) }}</div>
            </div>
          </v-progress-circular>

          <div class="flex-grow-1">
            <div
              v-for="cat in visibleCategories"
              :key="cat.name"
              class="mb-2"
            >
              <div class="d-flex align-center justify-space-between mb-1">
                <v-tooltip location="bottom">
                  <template #activator="{ props }">
                    <span v-bind="props" class="text-body-2 d-flex align-center ga-1" style="cursor: help">
                      <v-icon size="small">{{ categoryIcon(cat.name) }}</v-icon>
                      {{ categoryLabel(cat.name) }}
                    </span>
                  </template>
                  <div>
                    <div v-for="(val, key) in cat.details" :key="key" class="text-caption">
                      {{ detailLabel(String(key)) }}: {{ formatDetail(String(key), val) }}
                    </div>
                  </div>
                </v-tooltip>
                <span class="text-body-2 font-weight-medium">{{ Math.round(cat.score) }}</span>
              </div>
              <v-progress-linear
                :model-value="cat.score"
                :color="scoreColor(cat.score)"
                height="6"
                rounded
              />
            </div>
          </div>
        </div>
      </template>
    </v-card-text>
  </v-card>
</template>
