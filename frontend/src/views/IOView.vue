<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import IOBulkCard from '@/components/io/IOBulkCard.vue'
import IOCacheCard from '@/components/io/IOCacheCard.vue'
import IOContextChart from '@/components/io/IOContextChart.vue'
import IOMatrixTable from '@/components/io/IOMatrixTable.vue'
import IOModeBar from '@/components/io/IOModeBar.vue'
import IOVacuumCard from '@/components/io/IOVacuumCard.vue'
import { IO_MIN_VERSION_NUM, type MetricMode } from '@/components/io/types'
import { IO_RANGES, useIoHistory } from '@/components/io/useIoHistory'
import { useIoLive } from '@/components/io/useIoLive'
import { useIoRows } from '@/components/io/useIoRows'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useInstanceInfoStore } from '@/stores/instanceInfo'
import { useSnapshotsStatusStore } from '@/stores/snapshotsStatus'

// Poll intervals for live mode, in seconds; 0 means "only on demand".
const LIVE_INTERVAL_OPTIONS = [3, 5, 10, 30, 0]

const { t } = useI18n()
const { clusterName, hostName } = useClusterInfo()
const instanceInfo = useInstanceInfoStore()
const snapshotsStatus = useSnapshotsStatusStore()

snapshotsStatus.ensureLoaded()

const mode = ref<'history' | 'live'>('history')
const metricMode = ref<MetricMode>('count')
const range = ref<string>('6h')
const chartObject = ref<string>('relation')
const chartGroupBy = ref<'context' | 'backend_type'>('context')
const backendTypeFilter = ref<string | null>(null)
const contextFilter = ref<string | null>(null)
const showIdleBackends = ref(false)
const liveIntervalSec = ref(5)

const live = computed(() => mode.value === 'live')

const {
  history,
  matrix,
  loading: historyLoading,
  unavailable: historyUnavailable,
  load: loadHistoryData,
  clear: clearHistory,
} = useIoHistory()

const {
  rows: liveRows,
  windowSeconds: liveWindowSeconds,
  trackIoTiming: liveTrackIoTiming,
  waiting: liveWaiting,
  lastAt: liveLastAt,
  loading: liveLoading,
  unsupported,
  active: liveActive,
  remaining: liveRemaining,
  load: loadCurrent,
  reset: resetLive,
  start: startLive,
  stop: stopLive,
  toggle: toggleLive,
} = useIoLive({ clusterName, hostName, intervalSec: liveIntervalSec })

const {
  rows,
  bufferRows,
  availableObjects,
  backendTypes,
  windowSeconds,
  trackIoTiming,
  partial,
  noData,
} = useIoRows({
  live,
  liveRows,
  liveWindowSeconds,
  liveTrackIoTiming,
  history,
  matrix,
  backendType: backendTypeFilter,
  context: contextFilter,
})

const versionNum = computed(
  () => instanceInfo.known(clusterName.value ?? '', hostName.value ?? '')?.VersionNum ?? null,
)
const versionPending = computed(() =>
  instanceInfo.pending(clusterName.value ?? '', hostName.value ?? ''),
)
const versionSupported = computed(
  () => versionNum.value === null || versionNum.value >= IO_MIN_VERSION_NUM,
)

// The historical mode needs the snapshot store; without it the page opens live
// and the switch is hidden.
const historyAvailable = computed(() => snapshotsStatus.available && !historyUnavailable.value)

function loadHistory() {
  if (!clusterName.value || !hostName.value || live.value || !historyAvailable.value) return

  loadHistoryData({
    clusterName: clusterName.value,
    hostName: hostName.value,
    rangeKey: range.value,
    object: chartObject.value,
    groupBy: chartGroupBy.value,
    backendType: backendTypeFilter.value,
    context: contextFilter.value,
  })
}

// The toolbar sits above a long table; the FAB repeats its refresh so reading
// the matrix does not mean scrolling back up.
function refreshView() {
  if (live.value) loadCurrent()
  else loadHistory()
}

watch(historyUnavailable, (yes) => {
  if (yes) mode.value = 'live'
})

watch(
  [clusterName, hostName],
  ([cluster, host]) => {
    if (cluster && host) instanceInfo.ensure(cluster, host)

    resetLive()
    clearHistory()
  },
  { immediate: true },
)

// The store's `available` starts false and only becomes meaningful once the
// status has actually been fetched — switching to live before that would strand
// the user in live mode on every reload.
function applySnapshotsStatus(available: boolean) {
  if (!available && snapshotsStatus.cachedAt !== null) mode.value = 'live'
}

applySnapshotsStatus(snapshotsStatus.available)

// Not immediate: the mode watcher below owns the initial load, this one only
// reacts to a status that lands after it.
watch(
  () => snapshotsStatus.available,
  (available) => {
    applySnapshotsStatus(available)
    if (available && !live.value) loadHistory()
  },
)

watch(
  [mode, clusterName, hostName],
  () => {
    if (live.value) {
      resetLive()
      loadCurrent()
      if (liveIntervalSec.value > 0) startLive()
    } else {
      stopLive()
      loadHistory()
    }
  },
  { immediate: true },
)

watch(liveIntervalSec, (seconds) => {
  if (!live.value) return
  if (seconds > 0) startLive()
  else stopLive()
})

watch([range, chartObject, chartGroupBy, backendTypeFilter, contextFilter], () => loadHistory())

watch(availableObjects, (objects) => {
  if (!objects.includes(chartObject.value)) chartObject.value = 'relation'
})
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
      v-model:backend-type="backendTypeFilter"
      v-model:context="contextFilter"
      v-model:show-idle="showIdleBackends"
      v-model:live-interval-sec="liveIntervalSec"
      :history-available="historyAvailable"
      :backend-types="backendTypes"
      :ranges="Object.keys(IO_RANGES)"
      :track-io-timing="trackIoTiming"
      :live-active="liveActive"
      :live-remaining="liveRemaining"
      :live-loading="liveLoading"
      :live-interval-options="LIVE_INTERVAL_OPTIONS"
      :last-at="liveLastAt"
      :window-seconds="windowSeconds"
      @toggle-live="toggleLive"
      @refresh-live="loadCurrent"
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
        v-model:object="chartObject"
        v-model:group-by="chartGroupBy"
        :objects="availableObjects"
        :loading="historyLoading"
      />

      <v-row class="mb-4">
        <v-col cols="12" md="4">
          <IOCacheCard :rows="bufferRows" />
        </v-col>
        <v-col cols="12" md="4">
          <IOVacuumCard :rows="bufferRows" :window-seconds="windowSeconds" :metric-mode="metricMode" />
        </v-col>
        <v-col cols="12" md="4">
          <IOBulkCard :rows="bufferRows" :window-seconds="windowSeconds" :metric-mode="metricMode" />
        </v-col>
      </v-row>

      <IOMatrixTable
        class="mb-16"
        :rows="rows"
        :metric-mode="metricMode"
        :show-idle="showIdleBackends"
        :loading="historyLoading"
      />

      <v-alert v-if="noData && !historyLoading" type="info" variant="tonal" density="compact" class="mt-4">
        {{ t('io.noData') }}
      </v-alert>

      <v-fab
        icon="mdi-refresh"
        location="bottom end"
        size="small"
        app
        appear
        :title="t('autoRefresh.refresh')"
        :loading="mode === 'live' ? liveLoading : historyLoading"
        @click="refreshView"
      />
    </template>
  </template>
</template>
