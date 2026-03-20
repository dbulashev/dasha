<script setup lang="ts">
import { ref, watch, computed } from 'vue'
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
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import ProgressCard from '@/components/progress/ProgressCard.vue'
import type { ProgressCardData } from '@/components/progress/ProgressCard.vue'

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

// --- Helpers ---
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

function phaseLabel(type: ProgressCardData['type'], phase: string): string {
  const key = `progress.phases.${type}.${phase}`
  const translated = t(key)
  return translated === key ? phase : translated
}

// Phases where a numeric progress bar is meaningful
const analyzeProgressPhases = new Set(['acquiring sample rows'])
const clusterProgressPhases = new Set(['seq scanning heap', 'index scanning heap'])
const indexProgressPrefixes = ['building index', 'index validation: scanning index', 'index validation: scanning table']

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

// --- Unified card model ---
const cards = computed<ProgressCardData[]>(() => {
  const result: ProgressCardData[] = []

  for (const item of analyzeItems.value) {
    result.push({
      type: 'analyze', ...typeConfig.analyze, pid: item.Pid,
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
      type: 'vacuum', ...typeConfig.vacuum, pid: item.Pid,
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
      type: 'cluster', ...typeConfig.cluster, pid: item.Pid,
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
      type: 'index', ...typeConfig.index, pid: item.Pid,
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
      type: 'baseBackup', ...typeConfig.baseBackup, pid: item.Pid,
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
      errorMessage.value = getErrorMessage((rejected[0] as PromiseRejectedResult).reason)
    }
  } catch (err) {
    errorMessage.value = getErrorMessage(err)
  } finally {
    loading.value = false
  }
}

// --- Auto-refresh ---
const autoRefresh = useAutoRefresh({ onTick: loadEverything })

// --- Reload on cluster/host change ---
watch([clusterName, hostName], () => {
  autoRefresh.stop()
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
      :icon="autoRefresh.active.value ? 'mdi-stop' : 'mdi-play'"
      :color="autoRefresh.active.value ? 'error' : 'success'"
      variant="tonal"
      size="small"
      @click="autoRefresh.toggle"
    />
    <span v-if="autoRefresh.active.value" class="text-body-2 d-flex align-center ga-1">
      <v-icon size="small" color="success" class="auto-refresh-icon">mdi-refresh</v-icon>
      {{ autoRefresh.formatRemaining(autoRefresh.remaining.value) }}
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
  <ProgressCard
    v-for="card in cards"
    :key="`${card.type}-${card.pid}`"
    :card="card"
  />
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
