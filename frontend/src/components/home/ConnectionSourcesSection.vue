<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getConnectionSources } from '@/api/gen/default/default'
import type { ConnectionSource } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import PaginationControls from '@/components/PaginationControls.vue'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()

const emit = defineEmits<{ error: [msg: string] }>()

const PAGE_SIZE = 30
const headers = computed(() => [
  { title: t('header.database'), key: 'Database' },
  { title: t('header.user'), key: 'Username' },
  { title: t('home.applicationName'), key: 'ApplicationName' },
  { title: t('home.clientAddr'), key: 'ClientAddr' },
  { title: t('header.amount'), key: 'TotalConnections' },
])
const items = ref<ConnectionSource[]>([])
const loading = ref(false)
const hasMore = ref(true)
const page = ref(1)

async function load(p = 1) {
  if (!clusterName.value || !hostName.value) return
  loading.value = true
  page.value = p
  try {
    const response = await getConnectionSources({
      cluster_name: clusterName.value,
      instance: hostName.value,
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

watch([clusterName, hostName], () => load(), { immediate: true })
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('home.connectionSources') }}</v-card-title>
    <v-card-text>
      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
        density="compact"
        multi-sort
        disable-pagination
        hide-default-footer
        :no-data-text="t('noData')"
      />
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
