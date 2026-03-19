<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getConnectionStates } from '@/api/gen/default/default'
import type { ConnectionState } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()

const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.state'), key: 'State' },
  { title: t('header.amount'), key: 'Count' },
])
const items = ref<ConnectionState[]>([])
const loading = ref(false)

const totalConnections = computed(() =>
  items.value.reduce((sum, s) => sum + s.Count, 0),
)

async function load() {
  if (!clusterName.value || !hostName.value) return
  loading.value = true
  try {
    const response = await getConnectionStates({
      cluster_name: clusterName.value,
      instance: hostName.value,
    })
    items.value = assertOk(response) ?? []
  } catch (err) {
    emit('error', String(err))
    items.value = []
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName], () => load(), { immediate: true })
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
        density="compact"
        :items-per-page="-1"
        hide-default-footer
        :no-data-text="t('noData')"
      />
    </v-card-text>
  </v-card>
</template>
