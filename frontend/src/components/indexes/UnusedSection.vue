<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIndexesUnused } from '@/api/gen/default/default'
import type { IndexUnused } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import PaginationControls from '@/components/PaginationControls.vue'
import { fmtBytes } from '@/utils/format'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const PAGE_SIZE = 15
const headers = computed(() => [
  { title: t('header.schema'), key: 'Schema' },
  { title: t('header.table'), key: 'Table' },
  { title: t('header.index'), key: 'Index' },
  { title: t('header.size'), key: 'SizeBytes' },
  { title: t('header.indexScans'), key: 'IndexScans' },
])
const items = ref<IndexUnused[]>([])
const loading = ref(false)
const hasMore = ref(true)
const page = ref(1)
const allHosts = ref(false)

async function load(p = 1) {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  loading.value = true
  page.value = p
  try {
    const response = await getIndexesUnused({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      limit: PAGE_SIZE,
      offset: (p - 1) * PAGE_SIZE,
      all_hosts: allHosts.value || undefined,
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
watch(allHosts, () => load())
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('indexes.unused') }}</v-card-title>
    <v-card-text>
      <v-checkbox
        v-model="allHosts"
        :label="t('indexes.allHostsCheck')"
        density="compact"
        hide-details
        class="mb-2"
      />
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer>
        <template #item.SizeBytes="{ value }">{{ fmtBytes(value) }}</template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
