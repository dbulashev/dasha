<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getProgressAnalyze,
  getProgressVacuum,
  getProgressCluster,
  getProgressIndex,
  getProgressBaseBackup,
  getProgressCopy,
} from '@/api/gen/default/default'
import type {
  ProgressAnalyze,
  ProgressVacuum,
  ProgressCluster,
  ProgressIndex,
  ProgressBaseBackup,
  ProgressCopy,
} from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useViewError } from '@/composables/useViewError'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import AutoRefreshControls from '@/components/AutoRefreshControls.vue'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtBytes, fmtMs } from '@/utils/format'
import ProgressCard from '@/components/progress/ProgressCard.vue'
import type { ProgressCardData } from '@/components/progress/ProgressCard.vue'
import SlotForecastDialog from '@/components/progress/SlotForecastDialog.vue'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError, clearError } = useViewError()
const loading = ref(false)
const slotForecastOpen = ref(false)

// --- Data ---
const analyzeItems = ref<ProgressAnalyze[]>([])
const vacuumItems = ref<ProgressVacuum[]>([])
const clusterItems = ref<ProgressCluster[]>([])
const indexItems = ref<ProgressIndex[]>([])
const baseBackupItems = ref<ProgressBaseBackup[]>([])
const copyItems = ref<ProgressCopy[]>([])

// --- Helpers ---
const typeConfig = {
  analyze: { icon: 'mdi-magnify', label: 'progress.analyze' },
  vacuum: { icon: 'mdi-broom', label: 'progress.vacuum' },
  cluster: { icon: 'mdi-database-refresh', label: 'progress.cluster' },
  index: { icon: 'mdi-format-list-numbered', label: 'progress.index' },
  baseBackup: { icon: 'mdi-backup-restore', label: 'progress.baseBackup' },
  copy: { icon: 'mdi-database-import', label: 'progress.copy' },
}

function calcPct(done: number, total: number): number | null {
  if (!total || total <= 0) return null
  return Math.min(100, Math.round((done / total) * 100))
}

function fmtNum(v: number): string {
  return v.toLocaleString()
}

function fmtSeconds(v: number | null | undefined): string {
  return v == null ? '—' : fmtMs(v * 1000, t)
}

function phaseLabel(type: ProgressCardData['type'], phase: string): string {
  if (!phase) return '—'
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
        // PG < 17 caps collected dead tuples by row count (MaxDeadTuples);
        // PG 17+ caps by TID-store memory instead, shown as a separate metric.
        {
          label: t('progress.numDeadTuples'),
          value: item.MaxDeadTuples != null
            ? `${fmtNum(item.NumDeadTuples)} / ${fmtNum(item.MaxDeadTuples)}`
            : fmtNum(item.NumDeadTuples),
          hint: t('progress.numDeadTuplesHint'),
        },
        ...(item.DeadTupleBytes != null ? [{
          label: t('progress.deadTupleBytes'),
          value: `${fmtBytes(item.DeadTupleBytes)} / ${fmtBytes(item.MaxDeadTupleBytes)}`,
          hint: t('progress.deadTupleBytesHint'),
        }] : []),
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

  for (const item of copyItems.value) {
    result.push({
      type: 'copy', ...typeConfig.copy, pid: item.Pid,
      target: [item.Datname, item.TableName].filter(Boolean).join('.'),
      phase: item.Command,
      progress: item.ProgressPercentage ?? null,
      query: item.Query ?? undefined,
      metrics: [
        { label: t('progress.copyType'), value: item.Type },
        {
          label: t('progress.bytesProcessed'),
          value: item.BytesTotal != null
            ? `${fmtBytes(item.BytesProcessed)} / ${fmtBytes(item.BytesTotal)}`
            : fmtBytes(item.BytesProcessed),
          hint: t('progress.bytesProcessedHint'),
        },
        {
          label: t('progress.tuplesProcessed'),
          value: item.Command === 'COPY TO' && item.TableTuples != null
            ? `${fmtNum(item.TuplesProcessed)} / ${fmtNum(item.TableTuples)}`
            : fmtNum(item.TuplesProcessed),
          hint: t('progress.tuplesProcessedHint'),
        },
        ...(item.TuplesExcluded ? [{ label: t('progress.tuplesExcluded'), value: fmtNum(item.TuplesExcluded) }] : []),
        ...(item.TuplesSkipped ? [{ label: t('progress.tuplesSkipped'), value: fmtNum(item.TuplesSkipped) }] : []),
        ...(item.TableSize != null ? [{ label: t('progress.tableSize'), value: fmtBytes(item.TableSize) }] : []),
        { label: t('progress.elapsed'), value: fmtSeconds(item.ElapsedSeconds) },
        { label: t('progress.eta'), value: fmtSeconds(item.EtaSeconds), hint: t('progress.etaHint') },
      ],
    })
  }

  return result
})

// --- Data loading ---
async function loadEverything() {
  if (!clusterName.value || !hostName.value) return
  loading.value = true
  clearError()
  try {
    const [aRes, vRes, cRes, iRes, bRes, cpRes] = await Promise.allSettled([
      getProgressAnalyze({ cluster_name: clusterName.value, instance: hostName.value }),
      getProgressVacuum({ cluster_name: clusterName.value, instance: hostName.value }),
      getProgressCluster({ cluster_name: clusterName.value, instance: hostName.value }),
      getProgressIndex({ cluster_name: clusterName.value, instance: hostName.value }),
      getProgressBaseBackup({ cluster_name: clusterName.value, instance: hostName.value }),
      getProgressCopy({ cluster_name: clusterName.value, instance: hostName.value }),
    ])
    analyzeItems.value = aRes.status === 'fulfilled' ? assertOk(aRes.value) ?? [] : []
    vacuumItems.value = vRes.status === 'fulfilled' ? assertOk(vRes.value) ?? [] : []
    clusterItems.value = cRes.status === 'fulfilled' ? assertOk(cRes.value) ?? [] : []
    indexItems.value = iRes.status === 'fulfilled' ? assertOk(iRes.value) ?? [] : []
    baseBackupItems.value = bRes.status === 'fulfilled' ? assertOk(bRes.value) ?? [] : []
    copyItems.value = cpRes.status === 'fulfilled' ? assertOk(cpRes.value) ?? [] : []

    const rejected = [aRes, vRes, cRes, iRes, bRes, cpRes].filter(r => r.status === 'rejected')
    if (rejected.length) {
      const reason = (rejected[0] as PromiseRejectedResult).reason
      onError(getErrorMessage(reason), reason)
    }
  } catch (err) {
    onError(getErrorMessage(err), err)
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

  <!-- Header with auto-refresh control -->
  <div class="d-flex align-center ga-3 mb-4">
    <AutoRefreshControls
      :active="autoRefresh.active.value"
      :remaining="autoRefresh.remaining.value"
      :loading="loading"
      @toggle="autoRefresh.toggle"
      @refresh="loadEverything"
    />
    <v-spacer />
    <v-btn variant="tonal" prepend-icon="mdi-timer-sand" @click="slotForecastOpen = true">
      {{ t('slotForecast.button') }}
    </v-btn>
  </div>

  <SlotForecastDialog v-model="slotForecastOpen" />

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
</style>
