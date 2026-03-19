<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getMaintenanceInfo } from '@/api/gen/default/default'
import type { MaintenanceInfo } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.schema'), key: 'Schema' },
  { title: t('header.table'), key: 'Table' },
  { title: t('maintenance.lastVacuum'), key: 'LastVacuum' },
  { title: t('maintenance.lastAutovacuum'), key: 'LastAutovacuum' },
  { title: t('maintenance.lastAnalyze'), key: 'LastAnalyze' },
  { title: t('maintenance.lastAutoanalyze'), key: 'LastAutoanalyze' },
  { title: t('maintenance.deadRows'), key: 'DeadRows' },
  { title: t('maintenance.liveRows'), key: 'LiveRows' },
])
const items = ref<MaintenanceInfo[]>([])
const loading = ref(false)

function formatDateTime(iso: string | null): string {
  if (!iso) return '—'
  try {
    const d = new Date(iso)
    if (isNaN(d.getTime()) || d.getFullYear() < 2000) return '—'
    return d.toLocaleString()
  } catch {
    return iso
  }
}

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  loading.value = true
  try {
    const response = await getMaintenanceInfo({
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
    <v-card-title class="d-flex align-center ga-1">
      {{ t('maintenance.info') }}
      <v-tooltip :text="t('hint.maintenanceInfo')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer :no-data-text="t('noData')">
        <template #item.LastVacuum="{ value }">{{ formatDateTime(value) }}</template>
        <template #item.LastAutovacuum="{ value }">{{ formatDateTime(value) }}</template>
        <template #item.LastAnalyze="{ value }">{{ formatDateTime(value) }}</template>
        <template #item.LastAutoanalyze="{ value }">{{ formatDateTime(value) }}</template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
