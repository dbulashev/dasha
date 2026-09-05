<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  getQueryStatsStatus,
  postQueriesResetStats,
  getSnapshotsStatus,
  getSnapshots,
  postSnapshot,
  getSnapshot,
  getPgssStatsResetTime,
} from '@/api/gen/default/default'
import type { StatsResetTime } from '@/api/models/index'
import type { QueryStatsStatus, SnapshotListItem, QueryReport } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useViewError } from '@/composables/useViewError'
import { DEFAULT_STATS_SOURCE } from '@/composables/useStatsSource'
import { useAuthStore } from '@/stores/auth'
import { assertOk } from '@/utils/api'
import { fmtWindow, fmtDateTime } from '@/utils/format'
import { snapshotReasonI18nKey, snapshotCoverage, snapshotCovers, foreignStatsSource } from '@/utils/autosnapshot'
import QueryReportSection from '@/components/queries/QueryReportSection.vue'
import LockSnapshotDialog from '@/components/queries/LockSnapshotDialog.vue'
import ScopeSwitch from '@/components/queries/ScopeSwitch.vue'
import { useQueryScope } from '@/composables/useQueryScope'

const route = useRoute()
const router = useRouter()
const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { clearError, onError } = useViewError()
const authStore = useAuthStore()
const { scope, hasScopeChoice } = useQueryScope()

const queryStatsStatus = ref<QueryStatsStatus | null>(null)

const pgssUnavailable = computed(() => {
  if (!queryStatsStatus.value) return false
  return !queryStatsStatus.value.Available || !queryStatsStatus.value.Enabled || !queryStatsStatus.value.Readable
})

const currentStatsSource = computed(() => queryStatsStatus.value?.Source ?? '')

const statsSourceName = computed(() => currentStatsSource.value || DEFAULT_STATS_SOURCE)

const pgssWarningMessage = computed(() => {
  const s = queryStatsStatus.value
  if (!s) return ''
  const ext = s.Source || DEFAULT_STATS_SOURCE
  if (!s.Available) return t('pgssNotInstalled', { ext })
  if (!s.Enabled) return t('pgssNotEnabled', { ext })
  if (!s.Readable) return t('pgssNotReadable', { ext })
  return ''
})

const pgssRestricted = computed(() => {
  const s = queryStatsStatus.value
  return !!s && !pgssUnavailable.value && s.Restricted
})

const isAdmin = computed(() =>
  authStore.mode === 'none' || authStore.mode === 'token' || authStore.user?.role === 'admin'
)

const showResetButton = computed(() =>
  authStore.enableQueryStatsReset && isAdmin.value && !pgssUnavailable.value && queryStatsStatus.value
)

// --- Reset stats ---
const resetConfirmDialog = ref(false)
const resetting = ref(false)
const resetSnackbar = ref(false)
const resetSnackbarMsg = ref('')
const resetSnackbarColor = ref('success')

async function doReset() {
  resetConfirmDialog.value = false
  if (!clusterName.value || !hostName.value || !databaseName.value) return

  resetting.value = true
  try {
    const res = await postQueriesResetStats({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    if (res.status === 204) {
      resetSnackbarMsg.value = t('resetQueryStatsSuccess', { ext: statsSourceName.value })
      resetSnackbarColor.value = 'success'
      // The reset moved the pgss window start; refetch it so the live window
      // is not computed from the pre-reset timestamp.
      await loadLivePgssReset()
    } else if (res.status === 403) {
      resetSnackbarMsg.value = t('resetQueryStatsForbidden')
      resetSnackbarColor.value = 'warning'
    } else {
      resetSnackbarMsg.value = t('resetQueryStatsError')
      resetSnackbarColor.value = 'error'
    }
  } catch {
    resetSnackbarMsg.value = t('resetQueryStatsError')
    resetSnackbarColor.value = 'error'
  } finally {
    resetting.value = false
    resetSnackbar.value = true
  }
}

// --- Snapshots ---
const snapshotsAvailable = ref(false)
const snapshotsList = ref<SnapshotListItem[]>([])
const selectedSnapshotId = ref<string | null>(null)
const snapshotData = ref<QueryReport[] | null>(null)
const snapshotLoading = ref(false)
const snapshotCreating = ref(false)
const createWithLocks = ref(false)
const locksDialogOpen = ref(false)
const snapshotSnackbar = ref(false)
const snapshotSnackbarMsg = ref('')
const snapshotSnackbarColor = ref('success')

const isViewingSnapshot = computed(() => selectedSnapshotId.value !== null)

const showSnapshotButton = computed(() =>
  snapshotsAvailable.value && isAdmin.value && !pgssUnavailable.value && !isViewingSnapshot.value
)

const snapshotIdsSet = computed(() => new Set(snapshotsList.value.map(s => s.Id)))

const snapshotSelectItems = computed(() => {
  const live = { value: null as string | null, title: t('snapshotLiveData'), subtitle: '' }
  const items = snapshotsList.value.map((s) => {
    const foreign = foreignStatsSource(s.StatsSource, currentStatsSource.value)

    return {
      value: s.Id,
      title: `${fmtDateTime(s.CreatedAt)} · ${t(snapshotReasonI18nKey(s.Reason), s.Reason ?? 'manual')}`,
      // Snapshots are host-wide; the coverage line is what says whether the
      // database being browsed is inside a given one.
      subtitle: [snapshotCoverage(s.Databases) || t('snapshotNoAttribution'), foreign]
        .filter(Boolean)
        .join(' · '),
    }
  })
  return [live, ...items]
})

async function loadSnapshotsStatus() {
  try {
    const res = await getSnapshotsStatus()
    const body = assertOk<{ Available: boolean }>(res)
    snapshotsAvailable.value = body.Available
  } catch {
    snapshotsAvailable.value = false
  }
}

async function loadSnapshotsList() {
  if (!snapshotsAvailable.value || !clusterName.value || !hostName.value) {
    snapshotsList.value = []
    return
  }
  try {
    // Snapshots are host-wide: one holds every database of the instance.
    const res = await getSnapshots({
      cluster_name: clusterName.value,
      instance: hostName.value,
    })
    snapshotsList.value = assertOk<SnapshotListItem[]>(res) ?? []
  } catch {
    snapshotsList.value = []
  }
}

async function doCreateSnapshot() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  snapshotCreating.value = true
  try {
    const res = await postSnapshot({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      include_locks: createWithLocks.value,
    })
    if (res.status === 201) {
      snapshotSnackbarMsg.value = t('snapshotCreated')
      snapshotSnackbarColor.value = 'success'
      await loadSnapshotsList()
    } else {
      snapshotSnackbarMsg.value = t('snapshotError')
      snapshotSnackbarColor.value = 'error'
    }
  } catch {
    snapshotSnackbarMsg.value = t('snapshotError')
    snapshotSnackbarColor.value = 'error'
  } finally {
    snapshotCreating.value = false
    snapshotSnackbar.value = true
  }
}

async function loadSnapshotData(id: string) {
  snapshotLoading.value = true
  snapshotData.value = null
  try {
    const res = await getSnapshot(id, {
      database: databaseName.value ?? undefined,
      scope: scope.value,
    })
    snapshotData.value = assertOk<QueryReport[]>(res) ?? []
  } catch (err) {
    onError(String(err), err)
    snapshotData.value = null
  } finally {
    snapshotLoading.value = false
  }
}

function syncSnapshotToUrl(id: string | null) {
  const current = route.query.snapshot as string | undefined
  if ((id ?? undefined) !== (current ?? undefined)) {
    const query = { ...route.query }
    if (id) {
      query.snapshot = id
    } else {
      delete query.snapshot
    }
    router.replace({ query })
  }
}

watch(selectedSnapshotId, (id) => {
  syncSnapshotToUrl(id)
  if (id) {
    loadSnapshotData(id)
  } else {
    snapshotData.value = null
  }
})

// A stored snapshot is read server-side, so the scope switch has to refetch it.
watch([scope, databaseName], () => {
  if (selectedSnapshotId.value) loadSnapshotData(selectedSnapshotId.value)
})

const livePgssStatsReset = ref<string | null>(null)

async function loadLivePgssReset() {
  if (!clusterName.value || !hostName.value || !databaseName.value) {
    livePgssStatsReset.value = null
    return
  }
  try {
    const res = await getPgssStatsResetTime({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    const body = assertOk<StatsResetTime>(res)
    livePgssStatsReset.value = body?.Time ?? null
  } catch {
    livePgssStatsReset.value = null
  }
}

const statsWindowUnknown = computed(() => t('compare.statsWindowUnknown'))

const selectedSnapshot = computed(() =>
  selectedSnapshotId.value
    ? snapshotsList.value.find(s => s.Id === selectedSnapshotId.value) ?? null
    : null,
)

// Snapshots taken before per-database attribution hold instance-wide rows only.
const legacySnapshot = computed(() =>
  isViewingSnapshot.value && (selectedSnapshot.value?.JsonVersion ?? 2) < 2,
)

// Narrowing a snapshot to a database it never held leaves an empty page; say so
// instead of letting it read as "this database ran nothing".
const snapshotMissesDatabase = computed(() =>
  isViewingSnapshot.value &&
  !legacySnapshot.value &&
  scope.value === 'database' &&
  !snapshotCovers(selectedSnapshot.value?.Databases, databaseName.value),
)

// Without the switch on screen there is nothing to point the reader at.
const snapshotMissesDatabaseKey = computed(() =>
  hasScopeChoice.value ? 'snapshotMissesDatabase' : 'snapshotMissesDatabaseOnly',
)

// Span from the last pg_stat_statements reset to the moment the numbers were
// taken — how much history the report covers, not how old the snapshot is.
const statsWindowText = computed(() => {
  if (isViewingSnapshot.value && selectedSnapshot.value) {
    return fmtWindow(selectedSnapshot.value.PgssStatsReset ?? undefined, selectedSnapshot.value.CreatedAt, t, statsWindowUnknown.value)
  }
  if (!isViewingSnapshot.value && livePgssStatsReset.value) {
    return fmtWindow(livePgssStatsReset.value, new Date().toISOString(), t, statsWindowUnknown.value)
  }
  return ''
})

async function loadQueryStatsStatus() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  try {
    const response = await getQueryStatsStatus({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    queryStatsStatus.value = assertOk<QueryStatsStatus>(response)
  } catch {
    queryStatsStatus.value = null
  }
}

watch([clusterName, hostName, databaseName], async () => {
  clearError()
  snapshotData.value = null
  await loadQueryStatsStatus()
  await loadLivePgssReset()
  await loadSnapshotsStatus()
  await loadSnapshotsList()

  const urlSnapshot = route.query.snapshot as string | undefined

  // Cluster context may still be resolving (clusters store loads asynchronously,
  // so clusterName/hostName from useClusterInfo are null on the first run after an
  // SPA navigation). In that case loadSnapshotsList() returned an empty list — do
  // NOT treat the URL snapshot as "not found" or strip it from the URL yet. The
  // watch re-fires once clusterName/hostName settle, and the snapshot is restored.
  if (!clusterName.value || !hostName.value || !databaseName.value) {
    return
  }

  // Restore snapshot from URL or reset
  if (urlSnapshot && snapshotIdsSet.value.has(urlSnapshot)) {
    if (selectedSnapshotId.value === urlSnapshot) {
      await loadSnapshotData(urlSnapshot)
    } else {
      selectedSnapshotId.value = urlSnapshot
    }
  } else {
    if (urlSnapshot) {
      // Snapshot from URL not found — notify and clean URL
      snapshotSnackbarMsg.value = t('snapshotNotFound')
      snapshotSnackbarColor.value = 'warning'
      snapshotSnackbar.value = true
    }
    selectedSnapshotId.value = null
    syncSnapshotToUrl(null)
  }
}, { immediate: true })
</script>

<template>
  <v-alert v-if="pgssUnavailable" type="warning" class="mb-4" closable>{{ pgssWarningMessage }}</v-alert>
  <v-alert v-if="pgssRestricted" type="info" class="mb-4" closable>{{ t('pgssRestricted', { ext: statsSourceName }) }}</v-alert>

  <div class="d-flex align-center ga-2 mb-2 flex-wrap">
    <v-select
      v-if="snapshotsAvailable && snapshotsList.length"
      v-model="selectedSnapshotId"
      :items="snapshotSelectItems"
      :item-props="(item) => ({ subtitle: item.subtitle })"
      :label="t('snapshotSelect')"
      density="compact"
      variant="outlined"
      hide-details
      style="max-width: 300px;"
    />
    <ScopeSwitch :force-instance="legacySnapshot" />
    <v-tooltip v-if="statsWindowText" :text="t('compare.statsWindowHint', { ext: statsSourceName })" location="bottom" max-width="420">
      <template #activator="{ props: tp }">
        <span v-bind="tp" class="text-caption text-medium-emphasis d-inline-flex align-center ga-1">
          <v-icon size="small">mdi-timer-sand</v-icon>
          {{ t('compare.statsWindow') }}: {{ statsWindowText }}
        </span>
      </template>
    </v-tooltip>
    <v-btn
      v-if="isViewingSnapshot && selectedSnapshot && selectedSnapshot.HasLocks"
      variant="tonal"
      size="small"
      prepend-icon="mdi-lock-outline"
      @click="locksDialogOpen = true"
    >
      {{ t('autosnapshot.locks.open') }}
    </v-btn>
    <v-spacer />
    <v-checkbox
      v-if="showSnapshotButton"
      v-model="createWithLocks"
      :label="t('autosnapshot.locks.withLocks')"
      density="compact"
      hide-details
      class="flex-grow-0 mr-1"
    />
    <v-btn
      v-if="showSnapshotButton"
      color="primary"
      variant="outlined"
      size="small"
      prepend-icon="mdi-camera"
      :loading="snapshotCreating"
      @click="doCreateSnapshot"
    >
      {{ t('createSnapshot') }}
    </v-btn>
    <v-btn
      v-if="showResetButton && !isViewingSnapshot"
      color="error"
      variant="outlined"
      size="small"
      prepend-icon="mdi-delete-sweep"
      :loading="resetting"
      @click="resetConfirmDialog = true"
    >
      {{ t('resetQueryStats') }}
    </v-btn>
  </div>

  <v-alert v-if="snapshotMissesDatabase" type="info" class="mb-4">
    {{ t(snapshotMissesDatabaseKey, { database: databaseName, databases: snapshotCoverage(selectedSnapshot?.Databases) }) }}
  </v-alert>

  <v-progress-linear v-if="snapshotLoading" indeterminate class="mb-4" />
  <QueryReportSection v-else :snapshot-data="isViewingSnapshot ? snapshotData : undefined" />

  <LockSnapshotDialog v-model="locksDialogOpen" :snapshot-id="selectedSnapshotId" />

  <v-dialog v-model="resetConfirmDialog" max-width="420">
    <v-card>
      <v-card-text>{{ t('resetQueryStatsConfirm', { ext: statsSourceName }) }}</v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="resetConfirmDialog = false">{{ t('Cancel') }}</v-btn>
        <v-btn color="error" variant="flat" @click="doReset">{{ t('resetQueryStats') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <v-snackbar v-model="resetSnackbar" :color="resetSnackbarColor" :timeout="3000">
    {{ resetSnackbarMsg }}
  </v-snackbar>
  <v-snackbar v-model="snapshotSnackbar" :color="snapshotSnackbarColor" :timeout="3000">
    {{ snapshotSnackbarMsg }}
  </v-snackbar>
</template>
