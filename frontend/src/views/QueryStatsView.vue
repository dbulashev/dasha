<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQueryStatsStatus } from '@/api/gen/default/default'
import type { QueryStatsStatus } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useViewError } from '@/composables/useViewError'
import { assertOk } from '@/utils/api'
import QueryStatsChartSection from '@/components/queries/QueryStatsChartSection.vue'
import Top10ByTimeSection from '@/components/queries/Top10ByTimeSection.vue'
import Top10ByWalSection from '@/components/queries/Top10ByWalSection.vue'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { errorMessage, onError, clearError } = useViewError()

// --- Query stats status (shared warning) ---
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
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>
  <v-alert v-if="pgssUnavailable" type="warning" class="mb-4" closable>{{ pgssWarningMessage }}</v-alert>

  <QueryStatsChartSection @error="onError" />
  <Top10ByTimeSection @error="onError" />
  <Top10ByWalSection @error="onError" />
</template>
