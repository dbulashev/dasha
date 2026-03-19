<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getTablesPartitions } from '@/api/gen/default/default'
import type { TablePartition } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.parentSchema'), key: 'ParentSchema' },
  { title: t('header.parent'), key: 'Parent' },
  { title: t('header.childsCount'), key: 'ChildsCount' },
  { title: t('header.childsSize'), key: 'ChildsSizeBytes' },
  { title: t('header.childsAvgSize'), key: 'ChildsAvgSizeBytes' },
])
const items = ref<TablePartition[]>([])
const loading = ref(false)

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  loading.value = true
  try {
    const response = await getTablesPartitions({
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
      {{ t('tables.partitions') }}
      <v-tooltip :text="t('hint.partitions')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer>
        <template #item.ChildsSizeBytes="{ item }">{{ item.ChildsSize }}</template>
        <template #item.ChildsAvgSizeBytes="{ item }">{{ item.ChildsAvgSize }}</template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
