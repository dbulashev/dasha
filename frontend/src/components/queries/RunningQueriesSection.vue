<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQueriesRunning } from '@/api/gen/default/default'
import type { QueryRunning } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.pid'), key: 'Pid' },
  { title: t('header.state'), key: 'State' },
  { title: t('header.source'), key: 'Source' },
  { title: t('header.duration'), key: 'Duration' },
  { title: t('header.waiting'), key: 'Waiting' },
  { title: t('header.query'), key: 'Query' },
  { title: t('header.user'), key: 'User' },
  { title: t('header.backendType'), key: 'BackendType' },
])
const items = ref<QueryRunning[]>([])
const loading = ref(false)
const minDuration = ref(3)
const durationOptions = [3, 10, 100]

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  loading.value = true
  try {
    const response = await getQueriesRunning({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      min_duration: minDuration.value,
    })
    items.value = assertOk(response) ?? []
  } catch (err) {
    emit('error', String(err))
    items.value = []
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName, databaseName, minDuration], () => load(), { immediate: true })
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('Live Queries') }}</v-card-title>
    <v-card-text>
      <div class="d-flex align-center ga-4 mb-2">
        <v-select
          v-model="minDuration"
          :items="durationOptions"
          :label="t('queries.minDurationLabel')"
          density="compact"
          hide-details
          style="max-width: 200px"
        />
      </div>
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer />
    </v-card-text>
  </v-card>
</template>
