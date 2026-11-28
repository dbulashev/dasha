<script setup lang="ts">
import { useI18n } from 'vue-i18n'

export interface ProgressCardData {
  type: 'analyze' | 'vacuum' | 'cluster' | 'index' | 'baseBackup' | 'copy'
  icon: string
  label: string
  pid: number
  target: string
  phase: string
  progress: number | null
  query?: string
  metrics: { label: string; value: string; hint?: string }[]
  // Replaces the type label, e.g. REPACK instead of CLUSTER.
  title?: string
  chips?: { label: string; color: string; hint?: string }[]
  warning?: string
  phaseHint?: string
}

defineProps<{ card: ProgressCardData }>()

const { t } = useI18n()
</script>

<template>
  <v-card variant="outlined" class="mb-3">
    <v-card-title class="text-subtitle-1 d-flex align-center ga-2 flex-wrap">
      <v-icon :icon="card.icon" size="small" />
      <span class="font-weight-bold">{{ card.title ?? t(card.label) }}</span>
      <v-chip size="small" variant="tonal">PID {{ card.pid }}</v-chip>
      <template v-for="chip in card.chips ?? []" :key="chip.label">
        <v-tooltip v-if="chip.hint" :text="chip.hint" location="bottom" max-width="420">
          <template #activator="{ props }">
            <v-chip v-bind="props" size="small" variant="tonal" :color="chip.color">{{ chip.label }}</v-chip>
          </template>
        </v-tooltip>
        <v-chip v-else size="small" variant="tonal" :color="chip.color">{{ chip.label }}</v-chip>
      </template>
      <span v-if="card.target" class="text-body-2 text-medium-emphasis text-mono">{{ card.target }}</span>
    </v-card-title>

    <v-card-text class="pt-0">
      <v-alert v-if="card.warning" type="warning" variant="tonal" density="compact" class="mb-2">
        {{ card.warning }}
      </v-alert>
      <div class="d-flex align-center ga-2 mb-2">
        <v-chip size="small" color="info" variant="tonal">{{ card.phase }}</v-chip>
        <span v-if="card.phaseHint" class="text-caption text-medium-emphasis">{{ card.phaseHint }}</span>
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

      <div v-if="card.query" class="text-caption text-mono text-medium-emphasis text-truncate mb-2" :title="card.query">
        {{ card.query }}
      </div>

      <v-row dense>
        <v-col
          v-for="metric in card.metrics"
          :key="metric.label"
          cols="6"
          md="3"
        >
          <div class="text-caption text-medium-emphasis d-flex align-center ga-1">
            {{ metric.label }}
            <v-tooltip v-if="metric.hint" :text="metric.hint" location="bottom" max-width="420">
              <template #activator="{ props }">
                <v-icon v-bind="props" size="x-small">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-body-2">{{ metric.value }}</div>
        </v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>
