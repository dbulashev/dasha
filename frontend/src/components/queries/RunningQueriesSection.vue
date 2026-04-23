<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQueriesRunning } from '@/api/gen/default/default'
import type { QueryRunning } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()

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
const minDuration = ref(1000)
const durationOptions = [1000, 3000, 10000, 50000, 100000]

const { items, loading } = useApiLoader<QueryRunning[]>(
  () => getQueriesRunning({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
    min_duration: minDuration.value,
  }),
  {
    deps: [clusterName, hostName, databaseName, minDuration],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError,
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center">
      <v-icon start icon="mdi-play-circle-outline" />{{ t('Live Queries') }}
      <v-spacer />
      <v-select
        v-model="minDuration"
        :items="durationOptions"
        :label="t('queries.minDurationLabel')"
        density="compact"
        hide-details
        style="max-width: 200px"
      />
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" />
    </v-card-text>
  </v-card>
</template>
