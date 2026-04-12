<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQueryStatsStatus, postQueriesResetStats } from '@/api/gen/default/default'
import type { QueryStatsStatus } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useViewError } from '@/composables/useViewError'
import { useAuthStore } from '@/stores/auth'
import { assertOk } from '@/utils/api'
import QueryReportSection from '@/components/queries/QueryReportSection.vue'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { clearError } = useViewError()
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

const resetConfirmDialog = ref(false)
const resetting = ref(false)
const resetSnackbar = ref(false)
const resetSnackbarMsg = ref('')
const resetSnackbarColor = ref('success')

async function doReset() {
  resetConfirmDialog.value = false
  if (!clusterName.value || !hostName.value) return

  resetting.value = true
  try {
    const res = await postQueriesResetStats({
      cluster_name: clusterName.value,
      instance: hostName.value,
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

watch([clusterName, hostName, databaseName], () => {
  clearError()
  loadQueryStatsStatus()
}, { immediate: true })
</script>

<template>
  <v-alert v-if="pgssUnavailable" type="warning" class="mb-4" closable>{{ pgssWarningMessage }}</v-alert>

  <div v-if="showResetButton" class="d-flex justify-end mb-2">
    <v-btn
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

  <QueryReportSection />

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
</template>
