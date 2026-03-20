<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getTablesCaching } from '@/api/gen/default/default'
import type { TableCaching } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import { fmtPct } from '@/utils/format'
import PaginationControls from '@/components/PaginationControls.vue'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const PAGE_SIZE = 15
const headers = computed(() => [
  { title: t('header.schema'), key: 'Schema' },
  { title: t('header.table'), key: 'Table' },
  { title: t('tables.heapHitRate'), key: 'HitRate' },
  { title: t('tables.idxHitRate'), key: 'IdxHitRate' },
  { title: t('tables.toastHitRate'), key: 'ToastHitRate' },
  { title: t('tables.toastIdxHitRate'), key: 'ToastIdxHitRate' },
])
const items = ref<TableCaching[]>([])
const loading = ref(false)
const hasMore = ref(true)
const page = ref(1)

async function load(p = 1) {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  loading.value = true
  page.value = p
  try {
    const response = await getTablesCaching({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      limit: PAGE_SIZE,
      offset: (p - 1) * PAGE_SIZE,
    })
    items.value = assertOk(response) ?? []
    hasMore.value = items.value.length >= PAGE_SIZE
  } catch (err) {
    emit('error', String(err))
    items.value = []
    hasMore.value = false
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName, databaseName], () => load(), { immediate: true })
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      {{ t('tables.caching') }}
      <v-tooltip :text="t('hint.tableCaching')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer>
        <template #item.HitRate="{ value }">{{ fmtPct(value, 2) }}</template>
        <template #item.IdxHitRate="{ value }">{{ fmtPct(value, 2) }}</template>
        <template #item.ToastHitRate="{ value }">{{ fmtPct(value, 2) }}</template>
        <template #item.ToastIdxHitRate="{ value }">{{ fmtPct(value, 2) }}</template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
