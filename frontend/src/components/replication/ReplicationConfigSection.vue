<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getReplicationConfig } from '@/api/gen/default/default'
import type { ReplicationConfig } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const { items: config, loading } = useApiLoader<ReplicationConfig>(
  () => getReplicationConfig({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: (msg) => emit('error', msg),
  },
)

const syncStandbyNames = computed(() => config.value?.SynchronousStandbyNames ?? '')
const syncCommit = computed(() => config.value?.SynchronousCommit ?? '')

function syncCommitColor(val: string): string {
  if (val === 'on' || val === 'remote_apply') return 'success'
  if (val === 'remote_write') return 'info'
  if (val === 'local') return 'warning'
  if (val === 'off') return 'error'
  return 'default'
}
</script>

<template>
  <v-card class="mb-4" :loading="loading">
    <v-card-title><v-icon start icon="mdi-cog-sync-outline" />{{ t('replication.config') }}</v-card-title>
    <v-card-text v-if="!loading">
      <div class="d-flex flex-column ga-2">
        <div class="d-flex align-center ga-2">
          <code class="text-body-2">synchronous_standby_names</code>
          <span class="text-body-2">=</span>
          <v-tooltip v-if="syncStandbyNames" location="top" max-width="400">
            <template #activator="{ props }">
              <v-chip v-bind="props" size="small" variant="tonal" color="info">
                {{ syncStandbyNames }}
              </v-chip>
            </template>
            {{ t('replication.syncStandbyHint') }}
          </v-tooltip>
          <v-tooltip v-else location="top" max-width="400">
            <template #activator="{ props }">
              <v-chip v-bind="props" size="small" variant="tonal">
                {{ t('replication.syncStandbyEmpty') }}
              </v-chip>
            </template>
            {{ t('replication.syncStandbyHint') }}
          </v-tooltip>
        </div>
        <div class="d-flex align-center ga-2">
          <code class="text-body-2">synchronous_commit</code>
          <span class="text-body-2">=</span>
          <v-tooltip location="top" max-width="400">
            <template #activator="{ props }">
              <v-chip v-bind="props" size="small" variant="tonal" :color="syncCommitColor(syncCommit)">
                {{ syncCommit }}
              </v-chip>
            </template>
            {{ t('replication.syncCommitHint.' + syncCommit) }}
          </v-tooltip>
        </div>
      </div>
    </v-card-text>
  </v-card>
</template>
