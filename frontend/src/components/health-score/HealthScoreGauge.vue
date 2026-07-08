<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    score: number
    size?: number
    width?: number
    showLabel?: boolean
  }>(),
  { size: 100, width: 10, showLabel: true },
)

const { t } = useI18n()

// Coerce to a finite number so a malformed response never feeds NaN into the
// SVG transform (which throws an "Invalid keyframe value" warning).
const safeScore = computed(() => (Number.isFinite(props.score) ? props.score : 0))

// The red band (< 40) is what the backend's critical floor targets
// (health.criticalScoreCeiling = 30); keep these thresholds in sync with it.
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
</script>

<template>
  <v-progress-circular
    :model-value="safeScore"
    :color="scoreColor(safeScore)"
    :size="props.size"
    :width="props.width"
  >
    <div class="text-center">
      <div class="text-h5 font-weight-bold">{{ Math.round(safeScore) }}</div>
      <div v-if="props.showLabel" class="text-caption">{{ scoreLabel(safeScore) }}</div>
    </div>
  </v-progress-circular>
</template>
