<script setup lang="ts">
import { ref, watch, computed, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { getConnectionStatActivity } from '@/api/gen/default/default'
import type { ConnectionStatActivity } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { DEFAULT_PAGE_SIZE } from '@/constants/pagination'
import PaginationControls from '@/components/PaginationControls.vue'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()

const emit = defineEmits<{ error: [msg: string] }>()

const PAGE_SIZE = DEFAULT_PAGE_SIZE
const headers = computed(() => [
  { title: 'PID', key: 'Pid', sortable: false },
  { title: t('header.user') + '@' + t('header.database'), key: 'userDb', sortable: false },
  { title: t('home.applicationName'), key: 'ApplicationName', sortable: false },
  { title: t('home.clientAddr'), key: 'ClientAddr', sortable: false },
  { title: t('header.state'), key: 'State', sortable: false },
  { title: t('home.backendType'), key: 'BackendType', sortable: false },
])
const items = ref<ConnectionStatActivity[]>([])
const loading = ref(false)
const hasMore = ref(true)
const page = ref(1)
const filterUser = ref('')
const filterState = ref('')

let filterTimer: ReturnType<typeof setTimeout> | null = null
watch(filterUser, () => {
  if (filterTimer) clearTimeout(filterTimer)
  filterTimer = setTimeout(() => load(), 500)
})
onBeforeUnmount(() => {
  if (filterTimer) clearTimeout(filterTimer)
})

function filterParams() {
  const params: Record<string, string> = {}
  if (filterUser.value) params.username = filterUser.value
  if (filterState.value) params.state = filterState.value
  return params
}

async function load(p = 1) {
  if (!clusterName.value || !hostName.value) return
  loading.value = true
  page.value = p
  try {
    const response = await getConnectionStatActivity({
      cluster_name: clusterName.value,
      instance: hostName.value,
      limit: PAGE_SIZE,
      offset: (p - 1) * PAGE_SIZE,
      ...filterParams(),
    })
    const data = assertOk<ConnectionStatActivity[]>(response) ?? []
    items.value = data
    hasMore.value = data.length >= PAGE_SIZE
  } catch (err) {
    emit('error', getErrorMessage(err))
    items.value = []
    hasMore.value = false
  } finally {
    loading.value = false
  }
}

const stateOptions = [
  { title: t('home.allStates'), value: '' },
  { title: 'active', value: 'active' },
  { title: 'idle', value: 'idle' },
  { title: 'idle in transaction', value: 'idle in transaction' },
  { title: 'idle in transaction (aborted)', value: 'idle in transaction (aborted)' },
  { title: 'fastpath function call', value: 'fastpath function call' },
  { title: 'disabled', value: 'disabled' },
]

watch([clusterName, hostName], () => load(), { immediate: true })
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('home.statActivity') }}</v-card-title>
    <v-card-text>
      <div class="d-flex ga-3 mb-3" style="max-width: 500px">
        <v-text-field
          v-model="filterUser"
          :label="t('header.user')"
          density="compact"
          hide-details
          clearable
          variant="outlined"
        />
        <v-select
          v-model="filterState"
          :items="stateOptions"
          :label="t('header.state')"
          density="compact"
          hide-details
          variant="outlined"
          @update:model-value="load()"
        />
      </div>
      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
        density="compact"
        :items-per-page="-1"
        hide-default-footer
        :no-data-text="t('noData')"
      >
        <template #item.userDb="{ item }">
          {{ [item.UserName, item.Database].filter(Boolean).join('@') || '—' }}
        </template>
        <template #item.ClientAddr="{ item }">
          <span class="d-inline-flex align-center ga-1">
            <v-icon v-if="item.Ssl" color="success" size="x-small">mdi-lock</v-icon>
            {{ item.ClientAddr }}
          </span>
        </template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
