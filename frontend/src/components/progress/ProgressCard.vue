<script setup lang="ts">
import { useI18n } from 'vue-i18n'

export interface ProgressCardData {
  type: 'analyze' | 'vacuum' | 'cluster' | 'index' | 'baseBackup'
  icon: string
  label: string
  pid: number
  target: string
  phase: string
  progress: number | null
  metrics: { label: string; value: string }[]
}

defineProps<{ card: ProgressCardData }>()

const { t } = useI18n()
</script>

<template>
  <v-card variant="outlined" class="mb-3">
    <v-card-title class="text-subtitle-1 d-flex align-center ga-2 flex-wrap">
      <v-icon :icon="card.icon" size="small" />
      <span class="font-weight-bold">{{ t(card.label) }}</span>
      <v-chip size="small" variant="tonal">PID {{ card.pid }}</v-chip>
      <span v-if="card.target" class="text-body-2 text-medium-emphasis" style="font-family: monospace;">{{ card.target }}</span>
    </v-card-title>

    <v-card-text class="pt-0">
      <div class="d-flex align-center ga-2 mb-2">
        <v-chip size="small" color="info" variant="tonal">{{ card.phase }}</v-chip>
        <span v-if="card.progress != null" class="text-body-2 font-weight-medium">{{ card.progress }}%</span>
      </div>

      <v-progress-linear
        v-if="card.progress != null"
        :model-value="card.progress"
        color="primary"
        height="8"
        rounded
        class="mb-3"
      />

      <v-row dense>
        <v-col
          v-for="metric in card.metrics"
          :key="metric.label"
          cols="6"
          md="3"
        >
          <div class="text-caption text-medium-emphasis">{{ metric.label }}</div>
          <div class="text-body-2">{{ metric.value }}</div>
        </v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>
