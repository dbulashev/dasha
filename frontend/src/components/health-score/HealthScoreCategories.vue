<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { HealthScoreCategory } from '@/api/models/index'
import { fmtBytes, fmtNum } from '@/utils/format'

const props = defineProps<{
  categories: HealthScoreCategory[]
}>()

const { t } = useI18n()

const visibleCategories = computed(() =>
  props.categories.filter((c) => c.weight > 0),
)

// The red band (< 40) is what the backend's critical floor targets
// (health.criticalScoreCeiling = 30). Keep these thresholds in sync with the
// other scoreColor copies (HealthScoreGauge, HealthScoreDatabases) and the floor.
function scoreColor(score: number): string {
  if (score >= 95) return 'success'
  if (score >= 70) return 'warning'
  if (score >= 40) return 'orange'
  return 'error'
}

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
    horizon: 'mdi-axis-arrow',
    wal_checkpoint: 'mdi-file-document-edit',
    locks: 'mdi-lock-outline',
  }
  return icons[name] ?? 'mdi-circle'
}

function formatDetail(key: string, value: number): string {
  if (
    key === 'autovacuum_enabled' ||
    key === 'track_counts_enabled' ||
    key === 'track_io_timing_enabled'
  ) {
    return value ? t('healthScore.details.valEnabled') : t('healthScore.details.valDisabled')
  }
  if (key === 'wal_level_minimal_with_replicas') return t('healthScore.details.walMinimalReplicas')
  if (key === 'wal_level_logical_without_slots') return t('healthScore.details.walLogicalNoSlots')
  if (key === 'cache_hit_ratio') return value.toFixed(2) + '%'
  if (key.endsWith('_ratio')) return value.toFixed(2)
  if (key === 'max_lag_bytes') return fmtBytes(value)
  if (key.endsWith('_seconds') || key.endsWith('_hours')) return value.toFixed(1)
  return String(fmtNum(value))
}

function detailLabel(key: string): string {
  return t(`healthScore.details.${key}`, key)
}
</script>

<template>
  <div>
    <div v-for="cat in visibleCategories" :key="cat.name" class="mb-2">
      <div class="d-flex align-center justify-space-between mb-1">
        <v-tooltip location="bottom">
          <template #activator="{ props: tp }">
            <span v-bind="tp" class="text-body-2 d-flex align-center ga-1" style="cursor: help">
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
</template>
