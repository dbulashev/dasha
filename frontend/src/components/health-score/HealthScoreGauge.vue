<script setup lang="ts">
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
    :model-value="props.score"
    :color="scoreColor(props.score)"
    :size="props.size"
    :width="props.width"
  >
    <div class="text-center">
      <div class="text-h5 font-weight-bold">{{ Math.round(props.score) }}</div>
      <div v-if="props.showLabel" class="text-caption">{{ scoreLabel(props.score) }}</div>
    </div>
  </v-progress-circular>
</template>
