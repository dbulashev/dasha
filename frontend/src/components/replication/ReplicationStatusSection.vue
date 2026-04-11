<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getReplicationStatus } from '@/api/gen/default/default'
import type { ReplicationStatus } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import { fmtLag, fmtBytes } from '@/utils/format'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()

const headers = computed(() => [
  { title: t('replication.applicationName'), key: 'ApplicationName' },
  { title: t('replication.stateSyncState'), key: 'State' },
  { title: t('replication.replayLag'), key: 'ReplayLagSeconds' },
  { title: t('replication.replayLagBytes'), key: 'ReplayLagBytes' },
  { title: t('replication.writeLag'), key: 'WriteLagSeconds' },
  { title: t('replication.flushLag'), key: 'FlushLagSeconds' },
])

const { items, loading } = useApiLoader<ReplicationStatus[]>(
  () => getReplicationStatus({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError,
  },
)

function lagColor(seconds: number | undefined): string {
  if (seconds === undefined || seconds === 0) return ''
  if (seconds < 5) return 'text-success'
  if (seconds < 30) return 'text-warning'
  return 'text-error'
}

function stateColor(state: string | undefined): string {
  if (state === 'streaming') return 'success'
  if (state === 'catchup') return 'warning'
  if (state === 'startup' || state === 'backup') return 'info'
  return 'default'
}

function syncColor(sync: string | undefined): string {
  if (sync === 'sync') return 'success'
  if (sync === 'quorum') return 'info'
  if (sync === 'potential') return 'warning'
  return 'default'
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-title><v-icon start icon="mdi-database-sync-outline" />{{ t('replication.status') }}</v-card-title>
    <v-card-text>
      <v-alert v-if="!loading && items.length === 0" type="info" variant="tonal" class="mb-0">
        {{ t('replication.noReplicas') }}
      </v-alert>
      <v-data-table
        v-if="loading || items.length > 0"
        :headers="headers"
        :items="items"
        :loading="loading"
        item-value="Pid"
        show-expand
      >
        <template #item.ReplayLagSeconds="{ item }">
          <span :class="lagColor(item.ReplayLagSeconds)">
            {{ fmtLag(item.ReplayLagSeconds) }}
          </span>
        </template>
        <template #item.ReplayLagBytes="{ item }">
          {{ fmtBytes(item.ReplayLagBytes) }}
        </template>
        <template #item.WriteLagSeconds="{ item }">
          {{ fmtLag(item.WriteLagSeconds) }}
        </template>
        <template #item.FlushLagSeconds="{ item }">
          {{ fmtLag(item.FlushLagSeconds) }}
        </template>
        <template #item.State="{ item }">
          <v-tooltip location="top">
            <template #activator="{ props }">
              <v-chip v-bind="props" size="small" :color="stateColor(item.State)" variant="tonal" class="mr-1 mb-1">
                {{ item.State }}
              </v-chip>
            </template>
            {{ t('replication.stateHint.' + item.State) }}
          </v-tooltip>
          <v-tooltip location="top">
            <template #activator="{ props }">
              <v-chip v-bind="props" size="small" :color="syncColor(item.SyncState)" variant="tonal">
                {{ item.SyncState }}
              </v-chip>
            </template>
            {{ t('replication.syncHint.' + item.SyncState) }}
          </v-tooltip>
        </template>
        <template #expanded-row="{ columns, item }">
          <tr>
            <td :colspan="columns.length" class="py-2 expanded-cell">
              <div class="d-flex flex-wrap ga-4">
                <span v-if="item.ClientAddr" class="text-caption">
                  <strong>{{ t('replication.clientAddr') }}:</strong> {{ item.ClientAddr }}
                </span>
                <span class="text-caption">
                  <strong>{{ t('replication.slot') }}:</strong>
                  {{ item.SlotName || '—' }}
                </span>
                <span class="text-caption">
                  <strong>PID:</strong> {{ item.Pid }}
                </span>
                <span v-if="item.Usename" class="text-caption">
                  <strong>{{ t('header.user') }}:</strong> {{ item.Usename }}
                </span>
                <span v-if="item.SentLsn" class="text-caption">
                  <strong>sent_lsn:</strong> {{ item.SentLsn }}
                </span>
                <span v-if="item.ReplayLsn" class="text-caption">
                  <strong>replay_lsn:</strong> {{ item.ReplayLsn }}
                </span>
              </div>
            </td>
          </tr>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.expanded-cell {
  padding-left: 2.5rem !important;
}
</style>
