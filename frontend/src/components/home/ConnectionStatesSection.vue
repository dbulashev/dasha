<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getConnectionStates } from '@/api/gen/default/default'
import type { ConnectionState } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.state'), key: 'State' },
  { title: t('header.amount'), key: 'Count' },
])

const { items, loading } = useApiLoader<ConnectionState[]>(
  () => getConnectionStates({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: (msg) => emit('error', msg),
  },
)

const totalConnections = computed(() =>
  items.value.reduce((sum, s) => sum + s.Count, 0),
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-2">
      {{ t('home.connectionStates') }}
      <v-chip v-if="!loading" size="small" variant="tonal">
        {{ t('home.totalConnections') }}: {{ totalConnections }}
      </v-chip>
    </v-card-title>
    <v-card-text>
      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
      />
    </v-card-text>
  </v-card>
</template>
