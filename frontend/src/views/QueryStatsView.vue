<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQueryStatsStatus } from '@/api/gen/default/default'
import type { QueryStatsStatus } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useViewError } from '@/composables/useViewError'
import { DEFAULT_STATS_SOURCE } from '@/composables/useStatsSource'
import { assertOk } from '@/utils/api'
import QueryStatsChartSection from '@/components/queries/QueryStatsChartSection.vue'
import IoCpuScatterSection from '@/components/queries/IoCpuScatterSection.vue'
import Top10ByTimeSection from '@/components/queries/Top10ByTimeSection.vue'
import Top10ByWalSection from '@/components/queries/Top10ByWalSection.vue'
import ScopeSwitch from '@/components/queries/ScopeSwitch.vue'
import { useQueryScope } from '@/composables/useQueryScope'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { clearError } = useViewError()
const { hasScopeChoice } = useQueryScope()

const queryStatsStatus = ref<QueryStatsStatus | null>(null)

// Guards against out-of-order responses: a reply for the host the user has
// already left must not resurrect its warning on the one now on screen.
let reqId = 0

const pgssUnavailable = computed(() => {
  if (!queryStatsStatus.value) return false
  return !queryStatsStatus.value.Available || !queryStatsStatus.value.Enabled || !queryStatsStatus.value.Readable
})

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

const statsSourceName = computed(() => queryStatsStatus.value?.Source || DEFAULT_STATS_SOURCE)

async function loadQueryStatsStatus() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  const myId = ++reqId
  try {
    const response = await getQueryStatsStatus({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    if (myId !== reqId) return // superseded — leave state to the newer load
    queryStatsStatus.value = assertOk<QueryStatsStatus>(response)
  } catch {
    if (myId !== reqId) return
    queryStatsStatus.value = null
  }
}

watch([clusterName, hostName, databaseName], () => {
  clearError()
  reqId++ // whatever is in flight describes the previous host
  queryStatsStatus.value = null
  loadQueryStatsStatus()
}, { immediate: true })
</script>

<template>
  <v-alert v-if="pgssUnavailable" type="warning" class="mb-4" closable>{{ pgssWarningMessage }}</v-alert>
  <v-alert v-if="pgssRestricted" type="info" class="mb-4" closable>{{ t('pgssRestricted', { ext: statsSourceName }) }}</v-alert>
  <div v-if="hasScopeChoice" class="d-flex align-center ga-2 mb-2">
    <ScopeSwitch />
  </div>
  <QueryStatsChartSection />
  <IoCpuScatterSection />
  <Top10ByTimeSection />
  <Top10ByWalSection />
</template>
