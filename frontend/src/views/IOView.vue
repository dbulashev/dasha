<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { getIOCurrent, getIOHistory } from '@/api/gen/default/default'
import type { IOHistory, IOSnapshot } from '@/api/models/index'
import IOBulkCard from '@/components/io/IOBulkCard.vue'
import IOCacheCard from '@/components/io/IOCacheCard.vue'
import IOContextChart from '@/components/io/IOContextChart.vue'
import IOMatrixTable from '@/components/io/IOMatrixTable.vue'
import IOModeBar from '@/components/io/IOModeBar.vue'
import IOVacuumCard from '@/components/io/IOVacuumCard.vue'
import { useIoLive } from '@/components/io/useIoLive'
import { VISIBLE_OBJECTS, type MatrixRow, type MetricMode } from '@/components/io/types'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useViewError } from '@/composables/useViewError'
import { useInstanceInfoStore } from '@/stores/instanceInfo'
import { useSnapshotsStatusStore } from '@/stores/snapshotsStatus'
import { ApiError, assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

// pg_stat_io was added in PostgreSQL 16; below that the page has no subject.
const MIN_VERSION_NUM = 160000

const LIVE_INTERVAL_MS = 3000
const LIVE_MAX_DURATION_MS = 5 * 60 * 1000
const HISTORY_POINTS = 200

// range key -> span in seconds
const RANGES: Record<string, number> = {
  '1h': 3600,
  '6h': 6 * 3600,
  '24h': 24 * 3600,
  '7d': 7 * 24 * 3600,
}

const { t } = useI18n()
const { clusterName, hostName } = useClusterInfo()
const { onError } = useViewError()
const instanceInfo = useInstanceInfoStore()
const snapshotsStatus = useSnapshotsStatusStore()

snapshotsStatus.ensureLoaded()

const mode = ref<'history' | 'live'>('history')
const metricMode = ref<MetricMode>('count')
const range = ref<string>('6h')
const chartObject = ref<string>('relation')
const backendTypeFilter = ref<string | null>(null)
const contextFilter = ref<string | null>(null)
const showIdleBackends = ref(false)

const history = ref<IOHistory | null>(null)
const matrix = ref<IOHistory | null>(null)
const historyLoading = ref(false)
const historyUnavailable = ref(false)
const unsupported = ref(false)

const {
  push: pushLive,
  reset: resetLiveBuffer,
  rows: liveRows,
  windowSeconds: liveWindowSeconds,
  waiting: liveWaiting,
  lastAt: liveLastAt,
  trackIoTiming: liveTrackIoTiming,
} = useIoLive()

// Bumped on every buffer drop: a poll of the previous host must not land in the
// new baseline — the epoch check compares versions and stats_reset, which two
// different servers can share.
let liveId = 0

function resetLive() {
  liveId++
  resetLiveBuffer()
}

const {
  active: liveActive,
  remaining: liveRemaining,
  start: startLive,
  stop: stopLive,
  toggle: toggleLive,
  formatRemaining,
} = useAutoRefresh({
  pollInterval: LIVE_INTERVAL_MS,
  maxDuration: LIVE_MAX_DURATION_MS,
  onTick: () => loadCurrent(),
})

const versionNum = computed(
  () => instanceInfo.known(clusterName.value ?? '', hostName.value ?? '')?.VersionNum ?? null,
)
const versionPending = computed(() =>
  instanceInfo.pending(clusterName.value ?? '', hostName.value ?? ''),
)
const versionSupported = computed(
  () => versionNum.value === null || versionNum.value >= MIN_VERSION_NUM,
)

// The historical mode needs the snapshot store; without it the page opens live
// and the switch is hidden.
const historyAvailable = computed(() => snapshotsStatus.available && !historyUnavailable.value)

let requestId = 0

async function loadHistory() {
  if (!clusterName.value || !hostName.value || mode.value !== 'history' || !historyAvailable.value) {
    return
  }

  const id = ++requestId
  historyLoading.value = true

  const to = new Date()
  const from = new Date(to.getTime() - RANGES[range.value] * 1000)

  const common = {
    cluster_name: clusterName.value,
    instance: hostName.value,
    from: from.toISOString(),
    to: to.toISOString(),
  }

  try {
    const [series, detail] = await Promise.all([
      getIOHistory({
        ...common,
        group_by: 'context',
        points: HISTORY_POINTS,
        object: chartObject.value,
        backend_type: backendTypeFilter.value ?? undefined,
        context: contextFilter.value ?? undefined,
      }),
      // The matrix is the same endpoint collapsed to one interval; it stays
      // unfiltered so every dimension keeps its full set of values — the
      // filters narrow it on the client.
      getIOHistory({ ...common, group_by: 'full', points: 1 }),
    ])

    if (id !== requestId) return

    history.value = assertOk<IOHistory>(series)
    matrix.value = assertOk<IOHistory>(detail)
  } catch (err) {
    if (id !== requestId) return

    history.value = null
    matrix.value = null

    if (err instanceof ApiError && err.status === 501) {
      historyUnavailable.value = true
      mode.value = 'live'
      return
    }

    onError(getErrorMessage(err), err)
  } finally {
    if (id === requestId) historyLoading.value = false
  }
}

async function loadCurrent() {
  if (!clusterName.value || !hostName.value) return

  const id = liveId

  try {
    const res = await getIOCurrent({ cluster_name: clusterName.value, instance: hostName.value })

    if (id !== liveId) return

    pushLive(assertOk<IOSnapshot>(res))
    unsupported.value = false
  } catch (err) {
    if (id !== liveId) return

    if (err instanceof ApiError && err.status === 501) {
      unsupported.value = true
      return
    }

    onError(getErrorMessage(err), err)
  }
}

function resetBaseline() {
  resetLive()
  loadCurrent()
}

// A host without pg_stat_io answers nothing; polling it would only repeat 501.
watch(unsupported, (yes) => {
  if (yes) stopLive()
})

watch(
  [clusterName, hostName],
  ([cluster, host]) => {
    if (cluster && host) instanceInfo.ensure(cluster, host)

    resetLive()
    historyUnavailable.value = false
    unsupported.value = false
  },
  { immediate: true },
)

// The store's `available` starts false and only becomes meaningful once the
// status has actually been fetched — switching to live before that would strand
// the user in live mode on every reload.
watch(
  () => snapshotsStatus.available,
  (available) => {
    if (available) {
      if (mode.value === 'history') loadHistory()
      return
    }

    if (snapshotsStatus.cachedAt !== null) mode.value = 'live'
  },
  { immediate: true },
)

watch(
  [mode, clusterName, hostName],
  ([current]) => {
    if (current === 'live') {
      resetLive()
      loadCurrent()
      startLive()
    } else {
      stopLive()
      loadHistory()
    }
  },
  { immediate: true },
)

watch([range, chartObject, backendTypeFilter, contextFilter], () => loadHistory())

onBeforeUnmount(stopLive)

const historyRows = computed<MatrixRow[]>(() =>
  (matrix.value?.series ?? []).map((s) => ({
    backend_type: s.key.backend_type ?? '',
    object: s.key.object ?? '',
    context: s.key.context ?? '',
    values: { ...(s.points[0]?.values ?? {}) },
  })),
)

const rawRows = computed<MatrixRow[]>(() =>
  mode.value === 'live' ? liveRows.value : historyRows.value,
)

// v1 covers buffered relation I/O; PG 18's WAL rows wait for a card of their own.
const rows = computed(() =>
  rawRows.value.filter((r) => {
    if (!VISIBLE_OBJECTS.includes(r.object)) return false
    if (contextFilter.value && r.context !== contextFilter.value) return false
    if (backendTypeFilter.value && r.backend_type !== backendTypeFilter.value) return false
    return true
  }),
)

const windowSeconds = computed(() =>
  mode.value === 'live'
    ? liveWindowSeconds.value
    : (matrix.value?.series?.[0]?.points?.[0]?.duration_seconds ?? 0),
)

const backendTypes = computed(() => {
  const seen = new Set(
    rawRows.value.filter((r) => VISIBLE_OBJECTS.includes(r.object)).map((r) => r.backend_type),
  )
  return [...seen].sort()
})

const trackIoTiming = computed(() =>
  mode.value === 'live' ? liveTrackIoTiming.value : (history.value?.meta.track_io_timing ?? true),
)

const partial = computed(
  () =>
    mode.value === 'history' &&
    (history.value?.series ?? []).some((s) => s.points.some((p) => !p.complete)),
)

const noData = computed(() => rows.value.length === 0)
</script>

<template>
  <v-alert v-if="!versionSupported" type="info" variant="tonal" class="mb-4">
    {{ t('io.requiresVersion') }}
  </v-alert>

  <v-skeleton-loader v-else-if="versionPending" type="heading, image" />

  <template v-else>
    <IOModeBar
      v-model:mode="mode"
      v-model:metric-mode="metricMode"
      v-model:range="range"
      v-model:chart-object="chartObject"
      v-model:backend-type="backendTypeFilter"
      v-model:context="contextFilter"
      v-model:show-idle="showIdleBackends"
      :history-available="historyAvailable"
      :backend-types="backendTypes"
      :ranges="Object.keys(RANGES)"
      :track-io-timing="trackIoTiming"
      :live-active="liveActive"
      :live-remaining="formatRemaining(liveRemaining)"
      :last-at="liveLastAt"
      :window-seconds="windowSeconds"
      @toggle-live="toggleLive"
      @reset-baseline="resetBaseline"
    />

    <v-alert v-if="unsupported" type="info" variant="tonal" class="mb-4">
      {{ t('io.requiresVersion') }}
    </v-alert>

    <v-alert
      v-else-if="mode === 'live' && liveWaiting"
      type="info"
      variant="tonal"
      class="mb-4"
    >
      {{ t('io.waitingSecondTick') }}
    </v-alert>

    <template v-else>
      <v-alert v-if="partial" type="warning" variant="tonal" density="compact" class="mb-4">
        {{ t('io.partialIntervals') }}
      </v-alert>

      <IOContextChart
        v-if="mode === 'history'"
        :history="history"
        :metric-mode="metricMode"
        :object="chartObject"
        :loading="historyLoading"
      />

      <v-row>
        <v-col cols="12" md="4">
          <IOCacheCard :rows="rows" />
        </v-col>
        <v-col cols="12" md="4">
          <IOVacuumCard :rows="rows" :window-seconds="windowSeconds" :metric-mode="metricMode" />
        </v-col>
        <v-col cols="12" md="4">
          <IOBulkCard :rows="rows" :window-seconds="windowSeconds" :metric-mode="metricMode" />
        </v-col>
      </v-row>

      <IOMatrixTable
        :rows="rows"
        :metric-mode="metricMode"
        :show-idle="showIdleBackends"
        :loading="historyLoading"
      />

      <v-alert v-if="noData && !historyLoading" type="info" variant="tonal" density="compact" class="mt-4">
        {{ t('io.noData') }}
      </v-alert>
    </template>
  </template>
</template>
