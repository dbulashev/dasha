<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getAutosnapshotConfig,
  putAutosnapshotConfig,
  getAutosnapshotTriggerEvents,
  getAutosnapshotStatus,
} from '@/api/gen/default/default'
import type { AutoSnapshotConfig, AutoSnapshotStatus, TriggerEvent } from '@/api/models'
import { AuthInfoMode } from '@/api/models'
import { useAuthStore } from '@/stores/auth'
import { useAutosnapshotStatusStore } from '@/stores/autosnapshotStatus'
import { useViewError } from '@/composables/useViewError'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { outcomeI18nKey, triggerI18nKey } from '@/utils/autosnapshot'
import AutoSnapshotClustersTab from '@/components/autosnapshot/AutoSnapshotClustersTab.vue'
import PaginationControls from '@/components/PaginationControls.vue'

const { t } = useI18n()
const { onError, clearError } = useViewError()
const authStore = useAuthStore()
const statusStore = useAutosnapshotStatusStore()

const isAdmin = computed(
  () => authStore.mode === AuthInfoMode.none || authStore.user?.role === 'admin',
)

const tab = ref<'settings' | 'clusters' | 'history'>('settings')

// --- Status panel ---------------------------------------------------------

const status = ref<AutoSnapshotStatus | null>(null)
const lastEvent = ref<TriggerEvent | null>(null)

const daemonAlive = computed(() => status.value?.Leader?.IsAlive === true)

async function loadStatus() {
  try {
    status.value = assertOk<AutoSnapshotStatus>(await getAutosnapshotStatus())
  } catch {
    status.value = null
  }
  try {
    const body = assertOk<{ Items?: TriggerEvent[] }>(
      await getAutosnapshotTriggerEvents({ limit: 1, offset: 0 } as Parameters<
        typeof getAutosnapshotTriggerEvents
      >[0]),
    )
    lastEvent.value = body?.Items?.[0] ?? null
  } catch {
    lastEvent.value = null
  }
}

// --- Settings tab ---------------------------------------------------------

const cfg = ref<AutoSnapshotConfig | null>(null)
const cfgLoading = ref(false)
const saveOk = ref(false)
const formRef = ref<{ validate: () => Promise<{ valid: boolean }> } | null>(null)

const directionOptions = computed(() => [
  { value: 'both', title: t('autosnapshot.direction.both') },
  { value: 'master_to_replica', title: t('autosnapshot.direction.masterToReplica') },
  { value: 'replica_to_master', title: t('autosnapshot.direction.replicaToMaster') },
])

// Retention is stored in bytes but edited in GiB.
const GIB = 1024 ** 3
const retentionGiB = computed({
  get: () => (cfg.value ? Math.round((cfg.value.RetentionBytes / GIB) * 100) / 100 : 0),
  set: (v) => {
    if (cfg.value) cfg.value.RetentionBytes = Math.max(0, Math.round((Number(v) || 0) * GIB))
  },
})

// Go-duration validation (e.g. 30s, 5m, 1h30m).
const durationRe = /^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$/
const durationRule = (v: string) =>
  durationRe.test((v ?? '').trim()) || t('autosnapshot.invalidDuration')
const positiveRule = (v: number) => (Number(v) >= 0 ? true : t('autosnapshot.mustBePositive'))

async function loadConfig() {
  cfgLoading.value = true
  try {
    cfg.value = assertOk<AutoSnapshotConfig>(await getAutosnapshotConfig())
  } catch (e) {
    onError(getErrorMessage(e), e)
  } finally {
    cfgLoading.value = false
  }
}

async function saveConfig() {
  if (!cfg.value) return
  const v = await formRef.value?.validate()
  if (v && !v.valid) return
  clearError()
  saveOk.value = false
  try {
    await putAutosnapshotConfig(cfg.value)
    saveOk.value = true
    statusStore.invalidateCache()
    await statusStore.ensureLoaded()
    await loadStatus()
  } catch (e) {
    onError(getErrorMessage(e), e)
  }
}

// --- History tab ----------------------------------------------------------

const events = ref<TriggerEvent[]>([])
const total = ref(0)
const page = ref(1)
const perPage = 15
const histLoading = ref(false)
const filterCluster = ref('')
const filterOutcome = ref('')
const filterTriggerType = ref('')
const filterFrom = ref('')
const filterTo = ref('')

const hasMore = computed(() => page.value * perPage < total.value)

// Only snapshot_created and error are persisted now (skips are debug logs).
const outcomeValues = ['snapshot_created', 'error']
const triggerTypeValues = ['activity_spike', 'role_change']

const outcomeOptions = computed(() =>
  outcomeValues.map((v) => ({ value: v, title: t(outcomeI18nKey(v), v) })),
)
const triggerTypeOptions = computed(() =>
  triggerTypeValues.map((v) => ({ value: v, title: t(triggerI18nKey(v), v) })),
)

async function loadEvents() {
  histLoading.value = true
  try {
    const params: Record<string, unknown> = {
      limit: perPage,
      offset: (page.value - 1) * perPage,
    }
    if (filterCluster.value) params.cluster_name = filterCluster.value
    if (filterOutcome.value) params.outcome = filterOutcome.value
    if (filterTriggerType.value) params.trigger_type = filterTriggerType.value
    if (filterFrom.value) params.from = new Date(filterFrom.value + 'T00:00:00').toISOString()
    if (filterTo.value) params.to = new Date(filterTo.value + 'T23:59:59.999').toISOString()

    const body = assertOk<{ Items?: TriggerEvent[]; Total?: number }>(
      await getAutosnapshotTriggerEvents(
        params as Parameters<typeof getAutosnapshotTriggerEvents>[0],
      ),
    )
    events.value = body?.Items ?? []
    total.value = body?.Total ?? 0
  } catch (e) {
    onError(getErrorMessage(e), e)
    events.value = []
    total.value = 0
  } finally {
    histLoading.value = false
  }
}

function outcomeColor(o: string): string {
  if (o === 'snapshot_created') return 'success'
  if (o === 'error') return 'error'
  return 'warning'
}

function fmtDateTime(s?: string | null): string {
  return s ? new Date(s).toLocaleString() : '—'
}

// Deep link to the snapshot in Query Report (host/db restore the context).
function snapshotLink(item: TriggerEvent) {
  return {
    name: 'query-report',
    params: { clustername: item.ClusterName },
    query: {
      host: item.Instance,
      ...(item.Database ? { db: item.Database } : {}),
      snapshot: item.SnapshotId as string,
    },
  }
}

// Human-readable trigger_context: labelled rows, formatted values, hiding keys
// already shown in table columns (trigger, host).
const ctxHidden = new Set(['trigger', 'host'])
function contextRows(ctx: Record<string, unknown> | undefined) {
  if (!ctx) return []
  return Object.entries(ctx)
    .filter(([k]) => !ctxHidden.has(k))
    .map(([k, v]) => ({ label: t(`autosnapshot.context.${k}`, k), value: fmtCtxValue(k, v) }))
}
function fmtCtxValue(key: string, v: unknown): string {
  if (v == null) return '—'
  if (key === 'threshold_pct') return `+${v}%`
  if (key === 'baseline') return Number(v).toFixed(1)
  if (key === 'duration' || key === 'window_size') return String(v).replace(/(\d)\.\d+s/, '$1s')
  return String(v)
}

const historyHeaders = computed(() => [
  { title: t('autosnapshot.history.when'), key: 'CreatedAt' },
  { title: t('autosnapshot.history.cluster'), key: 'ClusterName' },
  { title: t('autosnapshot.history.instance'), key: 'Instance' },
  { title: t('autosnapshot.history.trigger'), key: 'TriggerType' },
  { title: t('autosnapshot.history.outcome'), key: 'Outcome' },
])

// Reset to first page when filters change, then (re)load.
watch([filterCluster, filterOutcome, filterTriggerType, filterFrom, filterTo], () => {
  page.value = 1
  loadEvents()
})
watch(page, () => loadEvents())

onMounted(() => {
  loadStatus()
  loadConfig()
  loadEvents()
})
</script>

<template>
  <div>
    <h1 class="text-h5 mb-4 d-flex align-center ga-2">
      <v-icon icon="mdi-camera-timer" />
      {{ t('autosnapshot.menu') }}
    </h1>

    <!-- Daemon status -->
    <v-card class="mb-4">
      <v-card-text class="d-flex align-center flex-wrap ga-4">
        <v-chip
          :color="status?.Enabled ? 'success' : undefined"
          :prepend-icon="status?.Enabled ? 'mdi-check-circle' : 'mdi-pause-circle'"
          size="small"
          label
        >
          {{ status?.Enabled ? t('autosnapshot.status.enabled') : t('autosnapshot.status.disabled') }}
        </v-chip>

        <v-chip
          :color="daemonAlive ? 'success' : 'warning'"
          :prepend-icon="daemonAlive ? 'mdi-heart-pulse' : 'mdi-heart-broken'"
          size="small"
          label
        >
          {{ daemonAlive ? t('autosnapshot.status.daemonActive') : t('autosnapshot.status.daemonDown') }}
        </v-chip>

        <span v-if="daemonAlive && status?.Leader?.LastHeartbeat" class="text-caption text-medium-emphasis">
          {{ t('autosnapshot.status.lastHeartbeat') }}: {{ fmtDateTime(status.Leader.LastHeartbeat) }}
        </span>

        <v-spacer />

        <span v-if="lastEvent" class="text-caption text-medium-emphasis d-flex align-center ga-2">
          {{ t('autosnapshot.status.lastEvent') }}:
          {{ fmtDateTime(lastEvent.CreatedAt) }}
          <v-chip :color="outcomeColor(lastEvent.Outcome)" size="x-small" label>
            {{ t(outcomeI18nKey(lastEvent.Outcome), lastEvent.Outcome) }}
          </v-chip>
        </span>
        <span v-else class="text-caption text-medium-emphasis">
          {{ t('autosnapshot.status.noEvents') }}
        </span>
      </v-card-text>
    </v-card>

    <v-tabs v-model="tab" color="primary" class="mb-4">
      <v-tab value="settings">{{ t('autosnapshot.tabs.settings') }}</v-tab>
      <v-tab value="clusters">{{ t('autosnapshot.tabs.clusters') }}</v-tab>
      <v-tab value="history">{{ t('autosnapshot.tabs.history') }}</v-tab>
    </v-tabs>

    <v-window v-model="tab">
      <!-- Settings -->
      <v-window-item value="settings">
        <v-skeleton-loader v-if="cfgLoading" type="card, card, card" />

        <v-form v-else-if="cfg" ref="formRef">
          <v-alert
            v-if="saveOk"
            type="success"
            variant="tonal"
            density="compact"
            class="mb-4"
            closable
            @click:close="saveOk = false"
          >
            {{ t('autosnapshot.saved') }}
          </v-alert>

          <v-card class="mb-4">
            <v-card-title class="text-subtitle-1">{{ t('autosnapshot.globalSection') }}</v-card-title>
            <v-card-text>
              <v-row dense>
                <v-col cols="12" sm="6" md="3">
                  <v-switch
                    v-model="cfg.Enabled"
                    :label="t('autosnapshot.enabled')"
                    :disabled="!isAdmin"
                    color="primary"
                    density="compact"
                    hide-details
                  />
                </v-col>
                <v-col cols="12" sm="6" md="3">
                  <v-text-field
                    v-model="cfg.PollInterval"
                    :label="t('autosnapshot.pollInterval')"
                    :disabled="!isAdmin"
                    :rules="[durationRule]"
                    density="compact"
                    placeholder="30s"
                  />
                </v-col>
                <v-col cols="12" sm="6" md="3">
                  <v-text-field
                    v-model="cfg.MaxSnapshotFrequency"
                    :label="t('autosnapshot.maxFrequency')"
                    :disabled="!isAdmin"
                    :rules="[durationRule]"
                    density="compact"
                    placeholder="1h"
                  />
                </v-col>
                <v-col cols="12" sm="6" md="3">
                  <v-text-field
                    v-model.number="cfg.MinBaselineActive"
                    :label="t('autosnapshot.minBaseline')"
                    :disabled="!isAdmin"
                    :rules="[positiveRule]"
                    type="number"
                    density="compact"
                  />
                </v-col>
                <v-col cols="12" sm="6" md="3">
                  <v-text-field
                    v-model.number="retentionGiB"
                    :label="t('autosnapshot.retentionGiB')"
                    :disabled="!isAdmin"
                    :rules="[positiveRule]"
                    :hint="t('autosnapshot.retentionBytesHint')"
                    type="number"
                    density="compact"
                    persistent-hint
                  />
                </v-col>
                <v-col cols="12" sm="6" md="3">
                  <v-text-field
                    v-model.number="cfg.RetentionMinDays"
                    :label="t('autosnapshot.retentionMinDays')"
                    :disabled="!isAdmin"
                    :rules="[positiveRule]"
                    type="number"
                    density="compact"
                  />
                </v-col>
              </v-row>
            </v-card-text>
          </v-card>

          <v-card class="mb-4">
            <v-card-title class="text-subtitle-1">{{ t('autosnapshot.activitySpike') }}</v-card-title>
            <v-card-text>
              <v-row dense>
                <v-col cols="12" sm="6" md="3">
                  <v-switch
                    v-model="cfg.Defaults.ActivitySpike.Enabled"
                    :label="t('autosnapshot.enabled')"
                    :disabled="!isAdmin"
                    color="primary"
                    density="compact"
                    hide-details
                  />
                </v-col>
                <v-col cols="12" sm="6" md="3">
                  <v-text-field
                    v-model="cfg.Defaults.ActivitySpike.WindowSize"
                    :label="t('autosnapshot.windowSize')"
                    :disabled="!isAdmin"
                    :rules="[durationRule]"
                    density="compact"
                    placeholder="5m"
                  />
                </v-col>
                <v-col cols="12" sm="6" md="3">
                  <v-text-field
                    v-model.number="cfg.Defaults.ActivitySpike.ActiveThresholdPct"
                    :label="t('autosnapshot.thresholdPct')"
                    :disabled="!isAdmin"
                    :rules="[positiveRule]"
                    type="number"
                    density="compact"
                    suffix="%"
                  />
                </v-col>
                <v-col cols="12" sm="6" md="3">
                  <v-text-field
                    v-model="cfg.Defaults.ActivitySpike.SpikeDuration"
                    :label="t('autosnapshot.spikeDuration')"
                    :disabled="!isAdmin"
                    :rules="[durationRule]"
                    density="compact"
                    placeholder="5m"
                  />
                </v-col>
              </v-row>
            </v-card-text>
          </v-card>

          <v-card class="mb-4">
            <v-card-title class="text-subtitle-1">{{ t('autosnapshot.roleChange') }}</v-card-title>
            <v-card-text>
              <v-row dense>
                <v-col cols="12" sm="6" md="3">
                  <v-switch
                    v-model="cfg.Defaults.RoleChange.Enabled"
                    :label="t('autosnapshot.enabled')"
                    :disabled="!isAdmin"
                    color="primary"
                    density="compact"
                    hide-details
                  />
                </v-col>
                <v-col cols="12" sm="6" md="4">
                  <v-select
                    v-model="cfg.Defaults.RoleChange.Direction"
                    :items="directionOptions"
                    :label="t('autosnapshot.direction.label')"
                    :disabled="!isAdmin"
                    density="compact"
                    hide-details
                  />
                </v-col>
              </v-row>
            </v-card-text>
          </v-card>

          <v-btn
            v-if="isAdmin"
            color="primary"
            variant="flat"
            prepend-icon="mdi-content-save"
            @click="saveConfig"
          >
            {{ t('autosnapshot.save') }}
          </v-btn>
        </v-form>
      </v-window-item>

      <!-- Clusters -->
      <v-window-item value="clusters">
        <AutoSnapshotClustersTab />
      </v-window-item>

      <!-- History -->
      <v-window-item value="history">
        <v-card class="mb-4">
          <v-card-text>
            <v-row dense>
              <v-col cols="12" sm="6" md="4">
                <v-text-field
                  v-model="filterCluster"
                  :label="t('autosnapshot.filter.cluster')"
                  density="compact"
                  hide-details
                  clearable
                />
              </v-col>
              <v-col cols="6" sm="6" md="2">
                <v-select
                  v-model="filterOutcome"
                  :items="outcomeOptions"
                  :label="t('autosnapshot.filter.outcome')"
                  density="compact"
                  hide-details
                  clearable
                />
              </v-col>
              <v-col cols="6" sm="6" md="2">
                <v-select
                  v-model="filterTriggerType"
                  :items="triggerTypeOptions"
                  :label="t('autosnapshot.filter.triggerType')"
                  density="compact"
                  hide-details
                  clearable
                />
              </v-col>
              <v-col cols="6" sm="6" md="2">
                <v-text-field
                  v-model="filterFrom"
                  :label="t('autosnapshot.filter.from')"
                  type="date"
                  density="compact"
                  hide-details
                  clearable
                />
              </v-col>
              <v-col cols="6" sm="6" md="2">
                <v-text-field
                  v-model="filterTo"
                  :label="t('autosnapshot.filter.to')"
                  type="date"
                  density="compact"
                  hide-details
                  clearable
                />
              </v-col>
            </v-row>
          </v-card-text>
        </v-card>

        <v-card>
          <v-data-table
            :headers="historyHeaders"
            :items="events"
            :loading="histLoading"
            item-value="Id"
            hover
            show-expand
            :expand-on-click="false"
            :items-per-page="perPage"
            hide-default-footer
          >
            <template #item.CreatedAt="{ item }">
              {{ fmtDateTime(item.CreatedAt) }}
            </template>
            <template #item.TriggerType="{ item }">
              {{ t(triggerI18nKey(item.TriggerType), item.TriggerType) }}
            </template>
            <template #item.Outcome="{ item }">
              <div class="d-flex align-center ga-2">
                <v-chip :color="outcomeColor(item.Outcome)" size="small" label>
                  {{ t(outcomeI18nKey(item.Outcome), item.Outcome) }}
                </v-chip>
                <v-btn
                  v-if="item.SnapshotId"
                  v-tooltip="t('autosnapshot.history.openSnapshot')"
                  icon="mdi-open-in-new"
                  size="x-small"
                  variant="text"
                  :to="snapshotLink(item)"
                />
              </div>
            </template>
            <template #expanded-row="{ columns, item }">
              <tr>
                <td :colspan="columns.length" class="pa-3">
                  <template v-if="contextRows(item.TriggerContext).length">
                    <div
                      v-for="row in contextRows(item.TriggerContext)"
                      :key="row.label"
                      class="d-flex text-body-2"
                    >
                      <span class="text-medium-emphasis" style="min-width: 220px">{{ row.label }}</span>
                      <span>{{ row.value }}</span>
                    </div>
                  </template>
                  <span v-else class="text-medium-emphasis">{{ t('autosnapshot.history.noContext') }}</span>
                  <div v-if="item.ErrorMessage" class="mt-2 text-error">{{ item.ErrorMessage }}</div>
                </td>
              </tr>
            </template>
          </v-data-table>

          <PaginationControls v-model:page="page" :has-more="hasMore" />
        </v-card>
      </v-window-item>
    </v-window>
  </div>
</template>
