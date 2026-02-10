<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getMaintenanceAutovacuumFreezeMaxAge } from '@/api/gen/default/default'
import type { MaintenanceAutovacuumFreezeMaxAge } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const items = ref<MaintenanceAutovacuumFreezeMaxAge[]>([])
const loading = ref(false)

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  loading.value = true
  try {
    const response = await getMaintenanceAutovacuumFreezeMaxAge({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    items.value = assertOk(response) ?? []
  } catch (err) {
    emit('error', String(err))
    items.value = []
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName, databaseName], () => load(), { immediate: true })
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('maintenance.autovacuumFreezeMaxAge') }}</v-card-title>
    <v-card-text>
      <v-progress-linear v-if="loading" indeterminate />
      <div v-else-if="items.length" class="d-flex flex-wrap ga-2">
        <v-chip v-for="(item, idx) in items" :key="idx" size="large" variant="tonal">
          autovacuum_freeze_max_age = {{ item.AutovacuumFreezeMaxAge }}
        </v-chip>
      </div>
      <div v-else class="text-medium-emphasis">{{ t('noData') }}</div>
    </v-card-text>
  </v-card>
</template>
