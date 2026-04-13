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
} from '@/api/gen/default/default'
import type { QueryStatsStatus, SnapshotListItem, QueryReport } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useViewError } from '@/composables/useViewError'
import { useAuthStore } from '@/stores/auth'
import { assertOk } from '@/utils/api'
import QueryReportSection from '@/components/queries/QueryReportSection.vue'

const route = useRoute()
const router = useRouter()
const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { clearError, onError } = useViewError()
const authStore = useAuthStore()

const queryStatsStatus = ref<QueryStatsStatus | null>(null)

const pgssUnavailable = computed(() => {
  if (!queryStatsStatus.value) return false
  return !queryStatsStatus.value.Available || !queryStatsStatus.value.Enabled || !queryStatsStatus.value.Readable
})

const pgssWarningMessage = computed(() => {
  const s = queryStatsStatus.value
  if (!s) return ''
  if (!s.Available) return t('pgssNotInstalled')
  if (!s.Enabled) return t('pgssNotEnabled')
  if (!s.Readable) return t('pgssNotReadable')
  return ''
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
      resetSnackbarMsg.value = t('resetQueryStatsSuccess')
      resetSnackbarColor.value = 'success'
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
const snapshotSnackbar = ref(false)
const snapshotSnackbarMsg = ref('')
const snapshotSnackbarColor = ref('success')

const isViewingSnapshot = computed(() => selectedSnapshotId.value !== null)

const showSnapshotButton = computed(() =>
  snapshotsAvailable.value && isAdmin.value && !pgssUnavailable.value && !isViewingSnapshot.value
)

const snapshotIdsSet = computed(() => new Set(snapshotsList.value.map(s => s.Id)))

const snapshotSelectItems = computed(() => {
  const live = { value: null as string | null, title: t('snapshotLiveData') }
  const items = snapshotsList.value.map(s => ({
    value: s.Id,
    title: new Date(s.CreatedAt).toLocaleString(),
  }))
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
  if (!snapshotsAvailable.value || !clusterName.value || !hostName.value || !databaseName.value) {
    snapshotsList.value = []
    return
  }
  try {
    const res = await getSnapshots({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
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
    const res = await getSnapshot(id)
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
  loadQueryStatsStatus()
  await loadSnapshotsStatus()
  await loadSnapshotsList()

  // Restore snapshot from URL or reset
  const urlSnapshot = route.query.snapshot as string | undefined
  if (urlSnapshot && snapshotIdsSet.value.has(urlSnapshot)) {
    if (selectedSnapshotId.value === urlSnapshot) {
      // Same ID — watcher won't fire, load data directly
      loadSnapshotData(urlSnapshot)
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

  <div class="d-flex align-center ga-2 mb-2 flex-wrap">
    <v-select
      v-if="snapshotsAvailable && snapshotsList.length"
      v-model="selectedSnapshotId"
      :items="snapshotSelectItems"
      :label="t('snapshotSelect')"
      density="compact"
      variant="outlined"
      hide-details
      style="max-width: 300px;"
    />
    <v-spacer />
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

  <v-progress-linear v-if="snapshotLoading" indeterminate class="mb-4" />
  <QueryReportSection v-else :snapshot-data="isViewingSnapshot ? snapshotData : undefined" />

  <v-dialog v-model="resetConfirmDialog" max-width="420">
    <v-card>
      <v-card-text>{{ t('resetQueryStatsConfirm') }}</v-card-text>
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
