<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { CONTEXTS, VISIBLE_OBJECTS, type MetricMode } from './types'
import { fmtDateTime } from '@/utils/format'

const props = defineProps<{
  historyAvailable: boolean
  backendTypes: string[]
  ranges: string[]
  trackIoTiming: boolean
  liveActive: boolean
  liveRemaining: string
  lastAt: string | null
  windowSeconds: number
}>()

defineEmits<{ (e: 'toggle-live'): void; (e: 'reset-baseline'): void }>()

const mode = defineModel<'history' | 'live'>('mode', { required: true })
const metricMode = defineModel<MetricMode>('metricMode', { required: true })
const range = defineModel<string>('range', { required: true })
const chartObject = defineModel<string>('chartObject', { required: true })
const backendType = defineModel<string | null>('backendType', { required: true })
const context = defineModel<string | null>('context', { required: true })
const showIdle = defineModel<boolean>('showIdle', { required: true })

const { t } = useI18n()

const contextOptions = computed(() => [
  { value: null, title: t('io.filter.allContexts') },
  ...CONTEXTS.map((c) => ({ value: c as string, title: c })),
])

const backendTypeOptions = computed(() => [
  { value: null, title: t('io.filter.allBackendTypes') },
  ...props.backendTypes.map((b) => ({ value: b, title: b })),
])

const objectOptions = VISIBLE_OBJECTS.map((o) => ({ value: o, title: o }))

const windowLabel = computed(() => {
  if (props.windowSeconds <= 0) return null
  if (props.windowSeconds < 90) return t('io.window.seconds', { n: Math.round(props.windowSeconds) })
  if (props.windowSeconds < 5400) return t('io.window.minutes', { n: Math.round(props.windowSeconds / 60) })
  return t('io.window.hours', { n: (props.windowSeconds / 3600).toFixed(1) })
})
</script>

<template>
  <v-card class="mb-4">
    <v-card-text class="d-flex align-center flex-wrap ga-3">
      <v-btn-toggle
        v-if="historyAvailable"
        v-model="mode"
        density="compact"
        variant="outlined"
        mandatory
      >
        <v-btn value="history" size="small" prepend-icon="mdi-chart-timeline-variant">
          {{ t('io.mode.history') }}
        </v-btn>
        <v-btn value="live" size="small" prepend-icon="mdi-pulse">{{ t('io.mode.live') }}</v-btn>
      </v-btn-toggle>

      <v-chip v-else size="small" variant="tonal" prepend-icon="mdi-information-outline">
        {{ t('io.historyUnavailable') }}
      </v-chip>

      <v-btn-toggle v-model="metricMode" density="compact" variant="outlined" mandatory>
        <v-btn value="count" size="small">{{ t('io.metric.count') }}</v-btn>
        <v-btn value="time" size="small" :disabled="!trackIoTiming">
          {{ t('io.metric.time') }}
          <v-tooltip v-if="!trackIoTiming" activator="parent" location="bottom" max-width="320">
            {{ t('io.trackIoTimingOff') }}
          </v-tooltip>
        </v-btn>
      </v-btn-toggle>

      <template v-if="mode === 'history'">
        <v-btn-toggle v-model="range" density="compact" variant="outlined" mandatory>
          <v-btn v-for="r in ranges" :key="r" :value="r" size="small">
            {{ t(`io.range.${r}`) }}
          </v-btn>
        </v-btn-toggle>

        <v-select
          v-model="chartObject"
          :items="objectOptions"
          :label="t('io.filter.chartObject')"
          density="compact"
          variant="outlined"
          hide-details
          style="max-width: 200px"
        />
      </template>

      <template v-else>
        <v-btn
          size="small"
          variant="tonal"
          :prepend-icon="liveActive ? 'mdi-pause' : 'mdi-play'"
          @click="$emit('toggle-live')"
        >
          {{ liveActive ? t('io.live.pause') : t('io.live.resume') }}
        </v-btn>
        <v-btn size="small" variant="text" prepend-icon="mdi-restart" @click="$emit('reset-baseline')">
          {{ t('io.live.resetBaseline') }}
        </v-btn>
        <span v-if="liveActive" class="text-caption text-medium-emphasis">
          {{ t('io.live.stopsIn', { time: liveRemaining }) }}
        </span>
        <span v-if="lastAt" class="text-caption text-medium-emphasis">
          {{ t('io.live.lastUpdate', { at: fmtDateTime(lastAt) }) }}
        </span>
      </template>

      <v-select
        v-model="backendType"
        :items="backendTypeOptions"
        :label="t('io.filter.backendType')"
        density="compact"
        variant="outlined"
        hide-details
        style="max-width: 220px"
      />

      <v-select
        v-model="context"
        :items="contextOptions"
        :label="t('io.filter.context')"
        density="compact"
        variant="outlined"
        hide-details
        style="max-width: 180px"
      />

      <v-checkbox
        v-model="showIdle"
        :label="t('io.filter.showIdle')"
        density="compact"
        hide-details
      />

      <v-spacer />

      <v-chip v-if="windowLabel" size="small" variant="tonal" prepend-icon="mdi-timer-outline">
        {{ t('io.window.label', { window: windowLabel }) }}
        <v-tooltip activator="parent" location="bottom" max-width="360">
          {{ t('io.window.hint') }}
        </v-tooltip>
      </v-chip>
    </v-card-text>
  </v-card>
</template>
