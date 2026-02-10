<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getReplicationSlots } from '@/api/gen/default/default'
import type { ReplicationSlot } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { fmtBytes } from '@/utils/format'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('replication.slotName'), key: 'SlotName' },
  { title: t('replication.slotType'), key: 'SlotType' },
  { title: t('header.database'), key: 'Database' },
  { title: t('replication.active'), key: 'Active' },
  { title: t('replication.walStatus'), key: 'WalStatus' },
  { title: t('replication.backlog'), key: 'BacklogBytes' },
  { title: t('replication.safeWalSize'), key: 'SafeWalSize' },
])

const { items, loading } = useApiLoader<ReplicationSlot[]>(
  () => getReplicationSlots({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: (msg) => emit('error', msg),
  },
)

function walStatusColor(status: string | undefined): string {
  if (!status) return 'default'
  if (status === 'reserved') return 'success'
  if (status === 'extended') return 'info'
  if (status === 'unreserved') return 'warning'
  if (status === 'lost') return 'error'
  return 'default'
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-title><v-icon start icon="mdi-tray-full" />{{ t('replication.slots') }}</v-card-title>
    <v-card-text>
      <v-alert v-if="!loading && items.length === 0" type="info" variant="tonal" class="mb-0">
        {{ t('replication.noSlots') }}
      </v-alert>
      <v-data-table
        v-else
        :headers="headers"
        :items="items"
        :loading="loading"
      >
        <template #item.Active="{ item }">
          <v-icon :color="item.Active ? 'success' : 'error'" size="small">
            {{ item.Active ? 'mdi-check-circle' : 'mdi-close-circle' }}
          </v-icon>
        </template>
        <template #item.WalStatus="{ item }">
          <v-tooltip v-if="item.WalStatus" location="top">
            <template #activator="{ props }">
              <v-chip v-bind="props" size="small" :color="walStatusColor(item.WalStatus)" variant="tonal">
                {{ item.WalStatus }}
              </v-chip>
            </template>
            {{ t('replication.walStatusHint.' + item.WalStatus) }}
          </v-tooltip>
        </template>
        <template #item.BacklogBytes="{ item }">
          {{ fmtBytes(item.BacklogBytes) }}
        </template>
        <template #item.SafeWalSize="{ item }">
          {{ fmtBytes(item.SafeWalSize) }}
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
