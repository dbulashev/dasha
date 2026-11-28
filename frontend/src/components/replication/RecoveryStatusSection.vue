<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { getReplicationRecovery } from '@/api/gen/default/default'
import type { RecoveryStatus } from '@/api/models/index'
import { PG19_VERSION_NUM } from '@/constants/pg'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useServerVersion } from '@/composables/useServerVersion'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtDateTime, fmtMs } from '@/utils/format'
import { pauseHighlighted, recoveryAttention } from './recovery'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const serverVersion = useServerVersion()

const visible = computed(
  () => serverVersion.info.value?.InRecovery === true && serverVersion.atLeast(PG19_VERSION_NUM),
)

const recovery = ref<RecoveryStatus | null>(null)
const loading = ref(false)
// Kept in the card: the global error would replace the whole page.
const error = ref<string | null>(null)

let requestId = 0

async function load() {
  // A previous host's pause state must not linger under the new host's name.
  const id = ++requestId
  recovery.value = null
  error.value = null
  loading.value = false
  if (!clusterName.value || !hostName.value || !visible.value) return

  loading.value = true
  try {
    const res = await getReplicationRecovery({ cluster_name: clusterName.value, instance: hostName.value })
    if (id !== requestId) return
    recovery.value = assertOk<RecoveryStatus>(res)
  } catch (err) {
    if (id !== requestId) return
    error.value = getErrorMessage(err)
  } finally {
    if (id === requestId) loading.value = false
  }
}

watch([clusterName, hostName, visible], load, { immediate: true })

const ok = computed(() => recovery.value?.state === 'ok')
const attention = computed(() => recoveryAttention(recovery.value))

function lsnTli(lsn: string | null | undefined, tli: number | null | undefined): string {
  if (!lsn) return '—'
  return tli == null ? lsn : `${lsn} (TLI ${tli})`
}

const lastXactAge = computed(() => {
  const s = recovery.value?.since_last_xact_seconds
  return s == null ? null : fmtMs(s * 1000, t)
})
</script>

<template>
  <v-card v-if="visible" class="mb-4" :color="attention ? 'warning' : undefined" :variant="attention ? 'tonal' : 'elevated'">
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <v-icon start icon="mdi-backup-restore" />
      <span>{{ t('replication.recovery.title') }}</span>
      <template v-if="ok">
        <v-chip
          v-if="recovery!.pause_state"
          size="small"
          variant="flat"
          :color="pauseHighlighted(recovery!.pause_state) ? 'warning' : 'success'"
        >
          {{ t(`replication.recovery.pauseState.${recovery!.pause_state}`, recovery!.pause_state) }}
        </v-chip>
        <v-chip v-if="recovery!.promote_triggered" size="small" variant="flat" color="error">
          {{ t('replication.recovery.promoteTriggered') }}
        </v-chip>
      </template>
    </v-card-title>
    <v-card-text>
      <v-progress-linear v-if="loading && !recovery" indeterminate />
      <v-alert v-else-if="error" type="error" variant="tonal" density="compact">{{ error }}</v-alert>
      <v-alert v-else-if="recovery?.state === 'not_in_recovery'" type="info" variant="tonal" density="compact">
        {{ t('replication.recovery.notInRecovery') }}
      </v-alert>
      <v-alert v-else-if="recovery?.state === 'no_privilege'" type="info" variant="tonal" density="compact">
        {{ t('replication.recovery.noPrivilege') }}
      </v-alert>
      <v-alert v-else-if="recovery?.state === 'unsupported_version'" type="info" variant="tonal" density="compact">
        {{ t('common.pg19Required', { feature: 'pg_stat_recovery' }) }}
      </v-alert>
      <v-row v-else-if="ok" dense>
        <v-col cols="12" sm="6" md="4">
          <div class="text-caption text-medium-emphasis">{{ t('replication.recovery.lastReplayed') }}</div>
          <div class="text-body-2 text-mono">{{ lsnTli(recovery!.last_replayed_end_lsn, recovery!.last_replayed_tli) }}</div>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <div class="text-caption text-medium-emphasis">{{ t('replication.recovery.replayEnd') }}</div>
          <div class="text-body-2 text-mono">{{ lsnTli(recovery!.replay_end_lsn, recovery!.replay_end_tli) }}</div>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <div class="text-caption text-medium-emphasis">{{ t('replication.recovery.lastXactTime') }}</div>
          <div class="text-body-2">
            {{ fmtDateTime(recovery!.recovery_last_xact_time) }}
            <span v-if="lastXactAge" class="text-medium-emphasis">
              ({{ t('replication.recovery.ago', { age: lastXactAge }) }})
            </span>
          </div>
        </v-col>
        <v-col cols="12" sm="6" md="4">
          <div class="text-caption text-medium-emphasis d-flex align-center ga-1">
            {{ t('replication.recovery.chunkStart') }}
            <v-tooltip :text="t('replication.recovery.chunkStartHint')" location="bottom" max-width="420">
              <template #activator="{ props }">
                <v-icon v-bind="props" size="x-small">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-body-2">{{ fmtDateTime(recovery!.current_chunk_start_time) }}</div>
        </v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>
