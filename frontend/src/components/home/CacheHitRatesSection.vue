<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getTablesHitRate, getIndexesHitRate } from '@/api/gen/default/default'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()

const emit = defineEmits<{ error: [msg: string] }>()

const HIT_RATE_THRESHOLD = 0.9
const tablesHitRate = ref<number | null>(null)
const indexesHitRate = ref<number | null>(null)
const loading = ref(false)

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  loading.value = true
  try {
    const [tablesRes, indexesRes] = await Promise.allSettled([
      getTablesHitRate({
        cluster_name: clusterName.value,
        instance: hostName.value,
        database: databaseName.value,
      }),
      getIndexesHitRate({
        cluster_name: clusterName.value,
        instance: hostName.value,
        database: databaseName.value,
      }),
    ])
    if (tablesRes.status === 'fulfilled') {
      const arr = assertOk<{ Rate: number }[]>(tablesRes.value)
      tablesHitRate.value = arr?.length ? arr[0].Rate : null
    } else {
      tablesHitRate.value = null
      emit('error', String(tablesRes.reason))
    }
    if (indexesRes.status === 'fulfilled') {
      const arr = assertOk<{ Rate: number }[]>(indexesRes.value)
      indexesHitRate.value = arr?.length ? arr[0].Rate : null
    } else {
      indexesHitRate.value = null
      emit('error', String(indexesRes.reason))
    }
  } catch (err) {
    emit('error', String(err))
    tablesHitRate.value = null
    indexesHitRate.value = null
  } finally {
    loading.value = false
  }
}

function hitRateColor(rate: number | null): string {
  if (rate === null) return 'default'
  return rate >= HIT_RATE_THRESHOLD ? 'success' : 'warning'
}

function formatHitRate(rate: number | null): string {
  if (rate === null) return '—'
  return (rate * 100).toFixed(2) + '%'
}

watch([clusterName, hostName, databaseName], () => load(), { immediate: true })
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      {{ t('home.cacheHitRates') }}
      <v-tooltip :text="t('hint.cacheHitRates')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-progress-linear v-if="loading" indeterminate />
      <div v-else class="d-flex flex-wrap ga-3">
        <v-chip :color="hitRateColor(tablesHitRate)" variant="tonal">
          {{ t('home.tablesHitRate') }}: {{ formatHitRate(tablesHitRate) }}
        </v-chip>
        <v-chip :color="hitRateColor(indexesHitRate)" variant="tonal">
          {{ t('home.indexesHitRate') }}: {{ formatHitRate(indexesHitRate) }}
        </v-chip>
        <v-chip
          v-if="(tablesHitRate !== null && tablesHitRate < HIT_RATE_THRESHOLD) || (indexesHitRate !== null && indexesHitRate < HIT_RATE_THRESHOLD)"
          color="warning"
          variant="tonal"
        >
          {{ t('home.lowHitRateWarning', { threshold: (HIT_RATE_THRESHOLD * 100).toFixed(0) }) }}
        </v-chip>
      </div>
    </v-card-text>
  </v-card>
</template>
