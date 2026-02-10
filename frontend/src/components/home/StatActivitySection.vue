<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getConnectionStatActivity } from '@/api/gen/default/default'
import type { ConnectionStatActivity } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { usePaginatedApiLoader } from '@/composables/useApiLoader'
import { DEFAULT_PAGE_SIZE } from '@/constants/pagination'
import PaginationControls from '@/components/PaginationControls.vue'
import { useDebouncedRef } from '@/composables/useDebouncedRef'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: 'PID', key: 'Pid', sortable: false },
  { title: t('header.user') + '@' + t('header.database'), key: 'userDb', sortable: false },
  { title: t('home.applicationName'), key: 'ApplicationName', sortable: false },
  { title: t('home.clientAddr'), key: 'ClientAddr', sortable: false },
  { title: t('header.state'), key: 'State', sortable: false },
  { title: t('home.backendType'), key: 'BackendType', sortable: false },
])

const filterUser = ref('')
const debouncedFilterUser = useDebouncedRef(filterUser, 500)
const filterState = ref('')

const stateOptions = [
  { title: t('home.allStates'), value: '' },
  { title: 'active', value: 'active' },
  { title: 'idle', value: 'idle' },
  { title: 'idle in transaction', value: 'idle in transaction' },
  { title: 'idle in transaction (aborted)', value: 'idle in transaction (aborted)' },
  { title: 'fastpath function call', value: 'fastpath function call' },
  { title: 'disabled', value: 'disabled' },
]

const { items, loading, page, hasMore, load } = usePaginatedApiLoader<ConnectionStatActivity>(
  (limit, offset) => getConnectionStatActivity({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    limit,
    offset,
    ...(debouncedFilterUser.value ? { username: debouncedFilterUser.value } : {}),
    ...(filterState.value ? { state: filterState.value } : {}),
  }),
  {
    pageSize: DEFAULT_PAGE_SIZE,
    deps: [clusterName, hostName, debouncedFilterUser, filterState],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: (msg) => emit('error', msg),
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center">
      <v-icon start icon="mdi-format-list-bulleted" />{{ t('home.statActivity') }}
      <v-spacer />
      <v-text-field
        v-model="filterUser"
        :label="t('header.user')"
        density="compact"
        hide-details
        clearable
        variant="outlined"
        style="max-width: 200px"
        class="mr-3"
      />
      <v-select
        v-model="filterState"
        :items="stateOptions"
        :label="t('header.state')"
        density="compact"
        hide-details
        variant="outlined"
        style="max-width: 200px"
      />
    </v-card-title>
    <v-card-text>
      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
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
