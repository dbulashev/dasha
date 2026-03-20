<script setup lang="ts">
import { ref, watch, computed, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getProgressAnalyze,
  getProgressVacuum,
  getProgressCluster,
  getProgressIndex,
  getProgressBaseBackup,
} from '@/api/gen/default/default'
import type {
  ProgressAnalyze,
  ProgressVacuum,
  ProgressCluster,
  ProgressIndex,
  ProgressBaseBackup,
} from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()

const errorMessage = ref('')
const loading = ref(false)

// --- Data ---
const analyzeItems = ref<ProgressAnalyze[]>([])
const vacuumItems = ref<ProgressVacuum[]>([])
const clusterItems = ref<ProgressCluster[]>([])
const indexItems = ref<ProgressIndex[]>([])
const baseBackupItems = ref<ProgressBaseBackup[]>([])

// --- Unified card model ---
interface ProgressCard {
  type: 'analyze' | 'vacuum' | 'cluster' | 'index' | 'baseBackup'
  icon: string
  label: string
  pid: number
  target: string
  phase: string
  progress: number | null // 0-100, null if not calculable
  metrics: { label: string; value: string }[]
}

const typeConfig = {
  analyze: { icon: 'mdi-magnify', label: 'progress.analyze' },
  vacuum: { icon: 'mdi-broom', label: 'progress.vacuum' },
  cluster: { icon: 'mdi-database-refresh', label: 'progress.cluster' },
  index: { icon: 'mdi-format-list-numbered', label: 'progress.index' },
  baseBackup: { icon: 'mdi-backup-restore', label: 'progress.baseBackup' },
}

function calcPct(done: number, total: number): number | null {
  if (!total || total <= 0) return null
  return Math.min(100, Math.round((done / total) * 100))
}

function fmtNum(v: number): string {
  return v.toLocaleString()
}

function phaseLabel(type: ProgressCard['type'], phase: string): string {
  const key = `progress.phases.${type}.${phase}`
  const translated = t(key)
  // If no translation found, vue-i18n returns the key itself — fall back to raw phase
  return translated === key ? phase : translated
}

// Phases where a numeric progress bar is meaningful
const analyzeProgressPhases = new Set(['acquiring sample rows'])
const clusterProgressPhases = new Set(['seq scanning heap', 'index scanning heap'])
const indexProgressPrefixes = [
  'building index',
  'index validation: scanning index',
  'index validation: scanning table',
]

function vacuumProgress(item: ProgressVacuum): number | null {
  if (item.Phase === 'scanning heap') return calcPct(item.HeapBlksScanned, item.HeapBlksTotal)
  if (item.Phase === 'vacuuming heap') return calcPct(item.HeapBlksVacuumed, item.HeapBlksTotal)
  return null
}

function clusterProgress(item: ProgressCluster): number | null {
  if (clusterProgressPhases.has(item.Phase)) return calcPct(item.HeapBlksScanned, item.HeapBlksTotal)
  return null
}

function indexProgress(item: ProgressIndex): number | null {
  if (!indexProgressPrefixes.some(p => item.Phase.startsWith(p))) return null
  return calcPct(item.BlocksDone, item.BlocksTotal) ?? calcPct(item.TuplesDone, item.TuplesTotal)
}

const cards = computed<ProgressCard[]>(() => {
  const result: ProgressCard[] = []

  for (const item of analyzeItems.value) {
    result.push({
      type: 'analyze',
      ...typeConfig.analyze,
      pid: item.Pid,
      target: [item.Datname, item.TableName].filter(Boolean).join('.'),
      phase: phaseLabel('analyze', item.Phase),
      progress: analyzeProgressPhases.has(item.Phase) ? calcPct(item.SampleBlksScanned, item.SampleBlksTotal) : null,
      metrics: [
        { label: t('progress.sampleBlksScanned'), value: `${fmtNum(item.SampleBlksScanned)} / ${fmtNum(item.SampleBlksTotal)}` },
        { label: t('progress.extStatsComputed'), value: `${fmtNum(item.ExtStatsComputed)} / ${fmtNum(item.ExtStatsTotal)}` },
        ...(item.CurrentChildTable ? [{ label: t('progress.currentChildTable'), value: item.CurrentChildTable }] : []),
      ],
    })
  }

  for (const item of vacuumItems.value) {
    result.push({
      type: 'vacuum',
      ...typeConfig.vacuum,
      pid: item.Pid,
      target: [item.Datname, item.TableName].filter(Boolean).join('.'),
      phase: phaseLabel('vacuum', item.Phase),
      progress: vacuumProgress(item),
      metrics: [
        { label: t('progress.heapBlksScanned'), value: `${fmtNum(item.HeapBlksScanned)} / ${fmtNum(item.HeapBlksTotal)}` },
        { label: t('progress.heapBlksVacuumed'), value: `${fmtNum(item.HeapBlksVacuumed)} / ${fmtNum(item.HeapBlksTotal)}` },
        { label: t('progress.numDeadTuples'), value: `${fmtNum(item.NumDeadTuples)} / ${fmtNum(item.MaxDeadTuples)}` },
        { label: t('progress.indexVacuumCount'), value: fmtNum(item.IndexVacuumCount) },
      ],
    })
  }

  for (const item of clusterItems.value) {
    result.push({
      type: 'cluster',
      ...typeConfig.cluster,
      pid: item.Pid,
      target: [item.Datname, item.TableName].filter(Boolean).join('.'),
      phase: phaseLabel('cluster', item.Phase),
      progress: clusterProgress(item),
      metrics: [
        { label: t('progress.command'), value: item.Command },
        { label: t('progress.heapBlksScanned'), value: `${fmtNum(item.HeapBlksScanned)} / ${fmtNum(item.HeapBlksTotal)}` },
        { label: t('progress.heapTuplesScanned'), value: fmtNum(item.HeapTuplesScanned) },
        { label: t('progress.heapTuplesWritten'), value: fmtNum(item.HeapTuplesWritten) },
        { label: t('progress.indexRebuildCount'), value: fmtNum(item.IndexRebuildCount) },
        ...(item.ClusterIndex ? [{ label: t('progress.clusterIndex'), value: item.ClusterIndex }] : []),
      ],
    })
  }

  for (const item of indexItems.value) {
    result.push({
      type: 'index',
      ...typeConfig.index,
      pid: item.Pid,
      target: [item.Datname, item.TableName, item.IndexName].filter(Boolean).join('.'),
      phase: phaseLabel('index', item.Phase),
      progress: indexProgress(item),
      metrics: [
        { label: t('progress.blocksDone'), value: `${fmtNum(item.BlocksDone)} / ${fmtNum(item.BlocksTotal)}` },
        { label: t('progress.tuplesDone'), value: `${fmtNum(item.TuplesDone)} / ${fmtNum(item.TuplesTotal)}` },
        { label: t('progress.lockersDone'), value: `${fmtNum(item.LockersDone)} / ${fmtNum(item.LockersTotal)}` },
        { label: t('progress.partitionsDone'), value: `${fmtNum(item.PartitionsDone)} / ${fmtNum(item.PartitionsTotal)}` },
      ],
    })
  }

  for (const item of baseBackupItems.value) {
    result.push({
      type: 'baseBackup',
      ...typeConfig.baseBackup,
      pid: item.Pid,
      target: '',
      phase: phaseLabel('baseBackup', item.Phase),
      progress: item.ProgressPercentage ?? null,
      metrics: [
        { label: t('progress.backupStreamed'), value: `${fmtNum(item.BackupStreamed)} / ${fmtNum(item.BackupTotal)}` },
        { label: t('progress.progressPct'), value: item.ProgressPercentage != null ? item.ProgressPercentage + '%' : '-' },
        { label: t('progress.tablespacesStreamed'), value: `${fmtNum(item.TablespacesStreamed)} / ${fmtNum(item.TablespacesTotal)}` },
      ],
    })
  }

  return result
})

// --- Data loading ---
async function loadEverything() {
  if (!clusterName.value || !hostName.value) return
  loading.value = true
  errorMessage.value = ''
  try {
    const [aRes, vRes, cRes, iRes, bRes] = await Promise.allSettled([
      getProgressAnalyze({ cluster_name: clusterName.value, instance: hostName.value }),
      getProgressVacuum({ cluster_name: clusterName.value, instance: hostName.value }),
      getProgressCluster({ cluster_name: clusterName.value, instance: hostName.value }),
      getProgressIndex({ cluster_name: clusterName.value, instance: hostName.value }),
      getProgressBaseBackup({ cluster_name: clusterName.value, instance: hostName.value }),
    ])
    analyzeItems.value = aRes.status === 'fulfilled' ? assertOk(aRes.value) ?? [] : []
    vacuumItems.value = vRes.status === 'fulfilled' ? assertOk(vRes.value) ?? [] : []
    clusterItems.value = cRes.status === 'fulfilled' ? assertOk(cRes.value) ?? [] : []
    indexItems.value = iRes.status === 'fulfilled' ? assertOk(iRes.value) ?? [] : []
    baseBackupItems.value = bRes.status === 'fulfilled' ? assertOk(bRes.value) ?? [] : []

    const rejected = [aRes, vRes, cRes, iRes, bRes].filter(r => r.status === 'rejected')
    if (rejected.length) {
      errorMessage.value = (rejected[0] as PromiseRejectedResult).reason?.toString() ?? 'API error'
    }
  } catch (err) {
    errorMessage.value = String(err)
  } finally {
    loading.value = false
  }
}

// --- Auto-refresh ---
const POLL_INTERVAL = 5000
const MAX_POLL_DURATION = 5 * 60 * 1000
const autoRefreshActive = ref(false)
const autoRefreshRemaining = ref(0)
let pollTimer: ReturnType<typeof setInterval> | null = null
let countdownTimer: ReturnType<typeof setInterval> | null = null
let pollStartTime = 0

function startAutoRefresh() {
  stopAutoRefresh()
  autoRefreshActive.value = true
  pollStartTime = Date.now()
  autoRefreshRemaining.value = MAX_POLL_DURATION

  pollTimer = setInterval(() => {
    const elapsed = Date.now() - pollStartTime
    if (elapsed >= MAX_POLL_DURATION) {
      stopAutoRefresh()
      return
    }
    loadEverything()
  }, POLL_INTERVAL)

  countdownTimer = setInterval(() => {
    const elapsed = Date.now() - pollStartTime
    autoRefreshRemaining.value = Math.max(0, MAX_POLL_DURATION - elapsed)
    if (autoRefreshRemaining.value <= 0) {
      stopAutoRefresh()
    }
  }, 1000)
}

function stopAutoRefresh() {
  autoRefreshActive.value = false
  autoRefreshRemaining.value = 0
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
}

function toggleAutoRefresh() {
  if (autoRefreshActive.value) {
    stopAutoRefresh()
  } else {
    startAutoRefresh()
  }
}

function formatRemaining(ms: number): string {
  const totalSec = Math.ceil(ms / 1000)
  const m = Math.floor(totalSec / 60)
  const s = totalSec % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

onBeforeUnmount(() => stopAutoRefresh())

// --- Reload on cluster/host change ---
watch([clusterName, hostName], () => {
  stopAutoRefresh()
  loadEverything()
}, { immediate: true })
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>
    {{ errorMessage }}
  </v-alert>

  <!-- Header with auto-refresh control -->
  <div class="d-flex align-center ga-3 mb-4">
    <v-btn
      :icon="autoRefreshActive ? 'mdi-stop' : 'mdi-play'"
      :color="autoRefreshActive ? 'error' : 'success'"
      variant="tonal"
      size="small"
      @click="toggleAutoRefresh"
    />
    <span v-if="autoRefreshActive" class="text-body-2 d-flex align-center ga-1">
      <v-icon size="small" color="success" class="auto-refresh-icon">mdi-refresh</v-icon>
      {{ formatRemaining(autoRefreshRemaining) }}
    </span>
    <v-btn
      icon="mdi-refresh"
      variant="text"
      size="small"
      :loading="loading"
      @click="loadEverything"
    />
  </div>

  <!-- Loading state -->
  <v-progress-linear v-if="loading && !cards.length" indeterminate class="mb-4" />

  <!-- No active operations -->
  <v-card v-if="!loading && cards.length === 0" variant="outlined" class="mb-4">
    <v-card-text class="d-flex align-center justify-center ga-2 py-8">
      <v-icon color="success" size="large">mdi-check-circle</v-icon>
      <span class="text-body-1">{{ t('progress.noActiveOps') }}</span>
    </v-card-text>
  </v-card>

  <!-- Operation cards -->
  <v-card
    v-for="card in cards"
    :key="`${card.type}-${card.pid}`"
    variant="outlined"
    class="mb-3"
  >
    <v-card-title class="text-subtitle-1 d-flex align-center ga-2 flex-wrap">
      <v-icon :icon="card.icon" size="small" />
      <span class="font-weight-bold">{{ t(card.label) }}</span>
      <v-chip size="small" variant="tonal">PID {{ card.pid }}</v-chip>
      <span v-if="card.target" class="text-body-2 text-medium-emphasis" style="font-family: monospace;">{{ card.target }}</span>
    </v-card-title>

    <v-card-text class="pt-0">
      <!-- Phase -->
      <div class="d-flex align-center ga-2 mb-2">
        <v-chip size="small" color="info" variant="tonal">{{ card.phase }}</v-chip>
        <span v-if="card.progress != null" class="text-body-2 font-weight-medium">{{ card.progress }}%</span>
      </div>

      <!-- Progress bar -->
      <v-progress-linear
        v-if="card.progress != null"
        :model-value="card.progress"
        color="primary"
        height="8"
        rounded
        class="mb-3"
      />

      <!-- Metrics -->
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

<style scoped>
.auto-refresh-icon {
  animation: spin 2s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
