<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { CONTEXTS, type MetricMode } from './types'
import AutoRefreshControls from '@/components/AutoRefreshControls.vue'
import { fmtDateTime } from '@/utils/format'

const props = defineProps<{
  historyAvailable: boolean
  backendTypes: string[]
  ranges: string[]
  trackIoTiming: boolean
  trackWalIoTiming: boolean
  liveActive: boolean
  liveRemaining: number
  liveLoading: boolean
  liveIntervalOptions: number[]
  lastAt: string | null
  windowSeconds: number
}>()

defineEmits<{ (e: 'toggle-live'): void; (e: 'refresh-live'): void }>()

const mode = defineModel<'history' | 'live'>('mode', { required: true })
const metricMode = defineModel<MetricMode>('metricMode', { required: true })
const range = defineModel<string>('range', { required: true })
const backendType = defineModel<string | null>('backendType', { required: true })
const context = defineModel<string | null>('context', { required: true })
const showIdle = defineModel<boolean>('showIdle', { required: true })
const liveIntervalSec = defineModel<number>('liveIntervalSec', { required: true })

const { t } = useI18n()

const contextOptions = computed(() => [
  { value: null, title: t('io.filter.allContexts') },
  ...CONTEXTS.map((c) => ({ value: c as string, title: c })),
])

const backendTypeOptions = computed(() => [
  { value: null, title: t('io.filter.allBackendTypes') },
  ...props.backendTypes.map((b) => ({ value: b, title: b })),
])

const timingHint = computed(() => {
  if (!props.trackIoTiming && !props.trackWalIoTiming) return t('io.trackIoTimingOff')
  if (!props.trackIoTiming) return t('io.trackIoTimingWalOnly')
  if (!props.trackWalIoTiming) return t('io.trackWalIoTimingOff')
  return null
})

const windowLabel = computed(() => {
  if (props.windowSeconds <= 0) return null
  if (props.windowSeconds < 90) return t('io.window.seconds', { n: Math.round(props.windowSeconds) })
  if (props.windowSeconds < 5400) return t('io.window.minutes', { n: Math.round(props.windowSeconds / 60) })
  return t('io.window.hours', { n: (props.windowSeconds / 3600).toFixed(1) })
})
</script>

<template>
  <v-card class="mb-4">
    <v-card-text class="d-flex flex-column ga-3">
      <div class="d-flex align-center flex-wrap ga-3">
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
          <v-btn value="time" size="small" :disabled="!trackIoTiming && !trackWalIoTiming">
            {{ t('io.metric.time') }}
            <v-tooltip v-if="timingHint" activator="parent" location="bottom" max-width="320">
              {{ timingHint }}
            </v-tooltip>
          </v-btn>
        </v-btn-toggle>

        <v-btn-toggle
          v-if="mode === 'history'"
          v-model="range"
          density="compact"
          variant="outlined"
          mandatory
        >
          <v-btn v-for="r in ranges" :key="r" :value="r" size="small">
            {{ t(`io.range.${r}`) }}
          </v-btn>
        </v-btn-toggle>

        <template v-else>
          <AutoRefreshControls
            v-model:interval-sec="liveIntervalSec"
            :active="liveActive"
            :remaining="liveRemaining"
            :loading="liveLoading"
            :interval-options="liveIntervalOptions"
            @toggle="$emit('toggle-live')"
            @refresh="$emit('refresh-live')"
          />
          <span v-if="lastAt" class="text-caption text-medium-emphasis">
            {{ t('io.live.lastUpdate', { at: fmtDateTime(lastAt) }) }}
          </span>
        </template>

        <v-spacer />

        <v-chip v-if="windowLabel" size="small" variant="tonal" prepend-icon="mdi-timer-outline">
          {{ t('io.window.label', { window: windowLabel }) }}
          <v-tooltip activator="parent" location="bottom" max-width="360">
            {{ t('io.window.hint') }}
          </v-tooltip>
        </v-chip>
      </div>

      <div class="d-flex align-center flex-wrap ga-3">
        <v-select
          v-model="backendType"
          :items="backendTypeOptions"
          :label="t('io.filter.backendType')"
          class="io-filter"
          density="compact"
          variant="outlined"
          hide-details
        />

        <v-select
          v-model="context"
          :items="contextOptions"
          :label="t('io.filter.context')"
          class="io-filter"
          density="compact"
          variant="outlined"
          hide-details
        />

        <v-checkbox
          v-model="showIdle"
          :label="t('io.filter.showIdle')"
          density="compact"
          hide-details
        />
      </div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.io-filter {
  flex: 0 1 220px;
  min-width: 170px;
}
</style>
