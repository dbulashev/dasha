<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import ActivityChart from '@/components/activity/ActivityChart.vue'
import ActivityTopTable from '@/components/activity/ActivityTopTable.vue'
import {
  ActivityBuffer,
  BACKGROUND,
  OTHER_EVENTS,
  queryKeyLabel,
  type Dimension,
  type Filters,
  type Range,
} from '@/components/activity/activityBuffer'
import {
  CLASS_INDEX,
  classColor,
  classHatch,
  eventClass,
  eventColor,
  OTHER_COLOR,
  SlotAssigner,
} from '@/components/activity/classes'
import { useActivitySampler, ERROR_STREAK_BANNER, IDLE_STOP_MS } from '@/composables/useActivitySampler'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useAuthStore } from '@/stores/auth'
import { fmtClockTime, fmtDecimal } from '@/utils/format'

const PERIOD_OPTIONS = [1, 2, 5, 10]
const TOP_EVENTS = 8
const VISIBLE_OPTIONS = [5, 15, 30, 60, 120, 240]

const { t } = useI18n()
const { clusterName, hostName } = useClusterInfo()
const auth = useAuthStore()

const windowMinutes = computed(() => auth.activityWindowMinutes)
const buffer = new ActivityBuffer(windowMinutes.value * 60_000)
watch(windowMinutes, (m) => (buffer.windowMs = m * 60_000))

// The buffer is not reactive; every change bumps the version.
const version = ref(0)

const periodSec = ref(1)
const visibleMinutes = ref(15)
const includeBackground = ref(false)
const showIdleTx = ref(true)
const byEvent = ref(false)
const selection = ref<Range | null>(null)
const filters = ref<Filters>({})
const dimension = ref<Dimension>('query')

const visibleOptions = computed(() => VISIBLE_OPTIONS.filter((m) => m <= windowMinutes.value))
watch(
  visibleOptions,
  (opts) => {
    if (!opts.includes(visibleMinutes.value)) visibleMinutes.value = opts[opts.length - 1] ?? 5
  },
  { immediate: true },
)

const { unsupported, lastError, errorStreak, access, idleStopped, paused, pause, resume } = useActivitySampler({
  cluster: clusterName,
  host: hostName,
  periodSec,
  buffer,
  onChange: () => version.value++,
})

watch(includeBackground, (v) => {
  buffer.includeBackground = v
  version.value++
})

watch([clusterName, hostName], () => {
  selection.value = null
  filters.value = {}
})

const hidden = computed(() => (showIdleTx.value ? new Set<number>() : new Set([CLASS_INDEX.ClientIdleTx])))

const visibleRange = computed<Range | null>(() => {
  void version.value
  const right = buffer.last
  if (right === null) return null
  return { from: right - visibleMinutes.value * 60_000, to: right + 1 }
})

const tableRange = computed(() => selection.value ?? visibleRange.value)

const chart = computed(() => {
  void version.value
  const range = visibleRange.value
  return range
    ? buffer.chart(range, filters.value, hidden.value, byEvent.value, TOP_EVENTS, periodSec.value * 1000)
    : null
})

const rows = computed(() => {
  void version.value
  const range = tableRange.value
  return range ? buffer.aggregate(range, filters.value, dimension.value, hidden.value) : []
})

const lockPids = computed(() => {
  void version.value
  const range = tableRange.value
  return range && dimension.value === 'session' ? buffer.pidsWithClass(range, 'Lock') : new Set<number>()
})

const truncated = computed(() => {
  void version.value
  return buffer.truncatedSeen
})

const totalAas = computed(() => rows.value.reduce((sum, r) => sum + r.aas, 0))

const queryInfo = (key: string) => buffer.queryInfo(key)
const sessionInfo = (pid: number) => buffer.sessionInfo(pid)

function classLabel(cls: string): string {
  return cls === 'ClientIdleTx' ? t('activity.class.ClientIdleTx') : cls
}

function eventLabel(value: string): string {
  if (value === OTHER_EVENTS) return t('activity.otherEvents')
  const i = value.indexOf(':')
  const cls = value.slice(0, i)
  const event = value.slice(i + 1)
  return event ? `${classLabel(cls)}: ${event}` : classLabel(cls)
}

// One assigner per class keeps an event on its shade while it stays on the chart.
const shadeAssigners = new Map<string, SlotAssigner>()
const eventShadeOf = computed(() => {
  const byClass = new Map<string, string[]>()
  for (const s of chart.value?.series ?? []) {
    if (s.key === OTHER_EVENTS) continue
    const cls = eventClass(s.key)
    byClass.set(cls, [...(byClass.get(cls) ?? []), s.key])
  }

  const out = new Map<string, number>()
  for (const [cls, assigner] of shadeAssigners) if (!byClass.has(cls)) assigner.assign([])
  for (const [cls, keys] of byClass) {
    let assigner = shadeAssigners.get(cls)
    if (!assigner) shadeAssigners.set(cls, (assigner = new SlotAssigner()))
    for (const [k, shade] of assigner.assign(keys)) out.set(k, shade)
  }
  return out
})

function seriesLabel(key: string): string {
  return byEvent.value ? eventLabel(key) : classLabel(key)
}

function seriesColor(key: string, dark: boolean): string {
  if (!byEvent.value) return classColor(key, dark)
  const shade = eventShadeOf.value.get(key)
  return shade === undefined ? OTHER_COLOR : eventColor(key, shade, dark)
}

function seriesHatch(key: string, dark: boolean): string | null {
  if (!byEvent.value) return classHatch(key, dark)
  return key === OTHER_EVENTS ? null : classHatch(eventClass(key), dark)
}

function onSelect(from: number, to: number) {
  selection.value = { from, to }
}

function addFilter(dim: Dimension, value: string) {
  const next = { ...filters.value }
  const set = new Set(next[dim] ?? [])
  set.add(value)
  next[dim] = set
  filters.value = next
}

function removeFilter(dim: Dimension, value: string) {
  const next = { ...filters.value }
  const set = new Set(next[dim] ?? [])
  set.delete(value)
  if (set.size) next[dim] = set
  else delete next[dim]
  filters.value = next
}

function filterLabel(dim: Dimension, value: string): string {
  if (dim === 'event') return eventLabel(value)
  if (value === BACKGROUND) return t('activity.background')
  if (dim === 'query') return queryKeyLabel(value) || t('activity.noText')
  return value || '—'
}

const chips = computed(() =>
  (Object.entries(filters.value) as [Dimension, Set<string>][]).flatMap(([dim, values]) =>
    [...values].map((value) => ({
      dim,
      value,
      label: `${t(`activity.dim.${dim}`)}: ${filterLabel(dim, value)}`,
    })),
  ),
)

function togglePause() {
  if (playing.value) pause()
  else resume()
}

const playing = computed(() => !paused.value && !idleStopped.value)

const switches = computed(() => [
  { model: includeBackground, label: t('activity.includeBackground'), hint: t('activity.includeBackgroundHint') },
  { model: showIdleTx, label: t('activity.showIdleTx'), hint: t('activity.showIdleTxHint') },
  { model: byEvent, label: t('activity.byEvent'), hint: t('activity.byEventHint') },
])

const selectionLabel = computed(() =>
  selection.value ? `${fmtClockTime(selection.value.from)} – ${fmtClockTime(selection.value.to)}` : '',
)
</script>

<template>
  <v-alert v-if="unsupported" type="info" variant="tonal" class="mb-4">
    {{ t('activity.requiresVersion') }}
  </v-alert>

  <template v-else>
    <v-card class="mb-4">
      <v-card-text class="d-flex flex-column ga-3">
        <div class="d-flex flex-wrap align-center ga-4">
          <div class="d-flex align-center ga-2">
            <v-btn
              :icon="playing ? 'mdi-pause' : 'mdi-play'"
              size="small"
              variant="tonal"
              :title="playing ? t('activity.pause') : t('activity.resume')"
              @click="togglePause"
            />
          </div>
          <v-select
            v-model="periodSec"
            :items="PERIOD_OPTIONS.map((s) => ({ title: t('activity.everySec', { n: s }), value: s }))"
            :label="t('activity.period')"
            density="compact"
            hide-details
            style="max-width: 160px"
          />
          <v-select
            v-model="visibleMinutes"
            :items="visibleOptions.map((m) => ({ title: t('activity.lastMin', { n: m }), value: m }))"
            :label="t('activity.visible')"
            density="compact"
            hide-details
            style="max-width: 210px"
          />
        </div>
        <div class="d-flex flex-wrap align-center gc-8 gr-1 ps-1">
          <v-tooltip v-for="sw in switches" :key="sw.label" :text="sw.hint" location="bottom" max-width="380">
            <template #activator="{ props: tip }">
              <v-switch v-bind="tip" v-model="sw.model.value" class="flex-grow-0" density="compact" hide-details color="primary">
                <template #label>
                  {{ sw.label }}
                  <v-icon icon="mdi-help-circle-outline" size="x-small" class="ml-1 text-medium-emphasis" />
                </template>
              </v-switch>
            </template>
          </v-tooltip>
        </div>
      </v-card-text>
    </v-card>

    <v-alert v-if="idleStopped" type="info" variant="tonal" density="compact" class="mb-4">
      <div class="d-flex flex-wrap align-center ga-2">
        <span>{{ t('activity.idleStoppedHint', { n: IDLE_STOP_MS / 60_000 }) }}</span>
        <v-spacer />
        <v-btn size="small" variant="tonal" prepend-icon="mdi-play" @click="togglePause">{{ t('activity.resume') }}</v-btn>
      </div>
    </v-alert>
    <v-alert v-if="errorStreak >= ERROR_STREAK_BANNER" type="error" variant="tonal" density="compact" class="mb-4">
      {{ t('activity.sampleFailing', { n: errorStreak }) }}: {{ lastError }}
    </v-alert>
    <v-alert v-if="access && !access.canReadAllStats" type="warning" variant="tonal" density="compact" class="mb-4">
      {{ t('activity.ownSessionsOnly') }}
    </v-alert>
    <v-alert v-if="truncated" type="warning" variant="tonal" density="compact" class="mb-4">
      {{ t('activity.truncated') }}
    </v-alert>
    <v-alert v-if="access && !access.queryIdAvailable" type="info" variant="tonal" density="compact" class="mb-4">
      {{ t('activity.computeQueryIdHint') }}
    </v-alert>

    <v-card class="mb-4">
      <v-card-title class="d-flex align-center ga-2">
        <v-icon start icon="mdi-chart-bar-stacked" />
        {{ t('activity.chartTitle') }}
        <v-tooltip :text="t('activity.aasHint')" location="bottom" max-width="380">
          <template #activator="{ props: tip }">
            <v-chip v-bind="tip" size="small" variant="tonal">
              {{ t('activity.aasTotal', { n: fmtDecimal(totalAas, 2) }) }}
            </v-chip>
          </template>
        </v-tooltip>
      </v-card-title>
      <v-card-subtitle class="text-wrap">{{ t('activity.chartHint') }}</v-card-subtitle>
      <v-card-text>
        <ActivityChart v-if="chart" :data="chart" :label="seriesLabel" :color="seriesColor" :hatch="seriesHatch" @select="onSelect" />
        <v-skeleton-loader v-else type="image" />
      </v-card-text>
    </v-card>

    <div v-if="selection || chips.length" class="d-flex flex-wrap ga-2 mb-4">
      <v-chip v-if="selection" closable color="primary" prepend-icon="mdi-select-compare" @click:close="selection = null">
        {{ selectionLabel }}
      </v-chip>
      <v-chip v-for="c in chips" :key="`${c.dim}:${c.value}`" closable @click:close="removeFilter(c.dim, c.value)">
        {{ c.label }}
      </v-chip>
    </div>

    <ActivityTopTable
      v-if="clusterName && hostName"
      v-model:dimension="dimension"
      class="mb-16"
      :rows="rows"
      :cluster="clusterName"
      :host="hostName"
      :track-utility="access?.trackUtility ?? true"
      :query-info="queryInfo"
      :session-info="sessionInfo"
      :lock-pids="lockPids"
      :event-label="eventLabel"
      @filter="addFilter"
    />
  </template>
</template>
