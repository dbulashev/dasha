<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getAutosnapshotConfig,
  putAutosnapshotConfig,
  getAutosnapshotTriggerEvents,
} from '@/api/gen/default/default'
import type {
  AutoSnapshotConfig,
  TriggerEvent,
} from '@/api/models'
import { AuthInfoMode } from '@/api/models'
import { useAuthStore } from '@/stores/auth'
import { useAutosnapshotStatusStore } from '@/stores/autosnapshotStatus'
import { assertOk } from '@/utils/api'
import { outcomeI18nKey, triggerI18nKey } from '@/utils/autosnapshot'
import AutoSnapshotClustersTab from '@/components/autosnapshot/AutoSnapshotClustersTab.vue'

const { t } = useI18n()
const authStore = useAuthStore()
const statusStore = useAutosnapshotStatusStore()

const isAdmin = computed(
  () => authStore.mode === AuthInfoMode.none || authStore.user?.role === 'admin',
)

const tab = ref<'settings' | 'clusters' | 'history'>('settings')

// --- Settings tab ---------------------------------------------------------

const cfg = ref<AutoSnapshotConfig | null>(null)
const cfgLoading = ref(false)
const cfgError = ref<string | null>(null)
const saveOk = ref(false)

const directionOptions = computed(() => [
  { value: 'both', title: t('autosnapshot.direction.both') },
  { value: 'master_to_replica', title: t('autosnapshot.direction.masterToReplica') },
  { value: 'replica_to_master', title: t('autosnapshot.direction.replicaToMaster') },
])

async function loadConfig() {
  cfgLoading.value = true
  cfgError.value = null
  try {
    const res = await getAutosnapshotConfig()
    cfg.value = assertOk<AutoSnapshotConfig>(res)
  } catch (e) {
    cfgError.value = String(e)
  } finally {
    cfgLoading.value = false
  }
}

async function saveConfig() {
  if (!cfg.value) return
  cfgError.value = null
  saveOk.value = false
  try {
    await putAutosnapshotConfig(cfg.value)
    saveOk.value = true
    statusStore.invalidateCache()
    await statusStore.ensureLoaded()
  } catch (e) {
    cfgError.value = String(e)
  }
}

// --- History tab ----------------------------------------------------------

const events = ref<TriggerEvent[]>([])
const total = ref(0)
const page = ref(1)
const perPage = 50
const histLoading = ref(false)
const filterCluster = ref('')
const filterOutcome = ref('')
const filterTriggerType = ref('')

const outcomeValues = [
  'snapshot_created',
  'skipped:debounce',
  'skipped:below_baseline',
  'skipped:wrong_direction',
  'skipped:storage_unavailable',
  'error',
]

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

    const res = await getAutosnapshotTriggerEvents(
      params as Parameters<typeof getAutosnapshotTriggerEvents>[0],
    )
    const body = assertOk<{ Items?: TriggerEvent[]; Total?: number }>(res)
    events.value = body?.Items ?? []
    total.value = body?.Total ?? 0
  } catch {
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

const historyHeaders = computed(() => [
  { title: t('autosnapshot.history.when'), key: 'CreatedAt' },
  { title: t('autosnapshot.history.cluster'), key: 'ClusterName' },
  { title: t('autosnapshot.history.instance'), key: 'Instance' },
  { title: t('autosnapshot.history.trigger'), key: 'TriggerType' },
  { title: t('autosnapshot.history.outcome'), key: 'Outcome' },
])

onMounted(() => {
  loadConfig()
  loadEvents()
})

watch([page, filterCluster, filterOutcome, filterTriggerType], () => {
  loadEvents()
})
</script>

<template>
  <div>
    <v-tabs v-model="tab" color="primary" class="mb-4">
      <v-tab value="settings">{{ t('autosnapshot.tabs.settings') }}</v-tab>
      <v-tab value="clusters">{{ t('autosnapshot.tabs.clusters') }}</v-tab>
      <v-tab value="history">{{ t('autosnapshot.tabs.history') }}</v-tab>
    </v-tabs>

    <v-window v-model="tab">
      <v-window-item value="settings">
        <v-progress-linear v-if="cfgLoading" indeterminate />
        <v-alert v-if="cfgError" type="error" class="mb-4">{{ cfgError }}</v-alert>
        <v-alert v-if="saveOk" type="success" class="mb-4" closable @click:close="saveOk = false">
          {{ t('autosnapshot.saved') }}
        </v-alert>

        <v-card v-if="cfg" class="mb-4">
          <v-card-title>{{ t('autosnapshot.globalSection') }}</v-card-title>
          <v-card-text>
            <v-row dense>
              <v-col cols="12" sm="6" md="3">
                <v-switch
                  v-model="cfg.Enabled"
                  :label="t('autosnapshot.enabled')"
                  :disabled="!isAdmin"
                  color="primary"
                  hide-details
                />
              </v-col>
              <v-col cols="12" sm="6" md="3">
                <v-text-field
                  v-model="cfg.PollInterval"
                  :label="t('autosnapshot.pollInterval')"
                  :disabled="!isAdmin"
                  density="compact"
                  hint="30s, 1m, 5m"
                  persistent-hint
                />
              </v-col>
              <v-col cols="12" sm="6" md="3">
                <v-text-field
                  v-model="cfg.MaxSnapshotFrequency"
                  :label="t('autosnapshot.maxFrequency')"
                  :disabled="!isAdmin"
                  density="compact"
                  hint="1h, 30m"
                  persistent-hint
                />
              </v-col>
              <v-col cols="12" sm="6" md="3">
                <v-text-field
                  v-model.number="cfg.MinBaselineActive"
                  :label="t('autosnapshot.minBaseline')"
                  :disabled="!isAdmin"
                  type="number"
                  density="compact"
                />
              </v-col>
              <v-col cols="12" sm="6" md="4">
                <v-text-field
                  v-model.number="cfg.RetentionBytes"
                  :label="t('autosnapshot.retentionBytes')"
                  :disabled="!isAdmin"
                  type="number"
                  density="compact"
                  :hint="t('autosnapshot.retentionBytesHint')"
                  persistent-hint
                />
              </v-col>
              <v-col cols="12" sm="6" md="4">
                <v-text-field
                  v-model.number="cfg.RetentionMinDays"
                  :label="t('autosnapshot.retentionMinDays')"
                  :disabled="!isAdmin"
                  type="number"
                  density="compact"
                />
              </v-col>
            </v-row>
          </v-card-text>
        </v-card>

        <v-card v-if="cfg" class="mb-4">
          <v-card-title>{{ t('autosnapshot.activitySpike') }}</v-card-title>
          <v-card-text>
            <v-row dense>
              <v-col cols="12" sm="6" md="3">
                <v-switch
                  v-model="cfg.Defaults.ActivitySpike.Enabled"
                  :label="t('autosnapshot.enabled')"
                  :disabled="!isAdmin"
                  color="primary"
                  hide-details
                />
              </v-col>
              <v-col cols="12" sm="6" md="3">
                <v-text-field
                  v-model="cfg.Defaults.ActivitySpike.WindowSize"
                  :label="t('autosnapshot.windowSize')"
                  :disabled="!isAdmin"
                  density="compact"
                  hint="5m"
                  persistent-hint
                />
              </v-col>
              <v-col cols="12" sm="6" md="3">
                <v-text-field
                  v-model.number="cfg.Defaults.ActivitySpike.ActiveThresholdPct"
                  :label="t('autosnapshot.thresholdPct')"
                  :disabled="!isAdmin"
                  type="number"
                  density="compact"
                />
              </v-col>
              <v-col cols="12" sm="6" md="3">
                <v-text-field
                  v-model="cfg.Defaults.ActivitySpike.SpikeDuration"
                  :label="t('autosnapshot.spikeDuration')"
                  :disabled="!isAdmin"
                  density="compact"
                  hint="5m"
                  persistent-hint
                />
              </v-col>
            </v-row>
          </v-card-text>
        </v-card>

        <v-card v-if="cfg" class="mb-4">
          <v-card-title>{{ t('autosnapshot.roleChange') }}</v-card-title>
          <v-card-text>
            <v-row dense>
              <v-col cols="12" sm="6" md="3">
                <v-switch
                  v-model="cfg.Defaults.RoleChange.Enabled"
                  :label="t('autosnapshot.enabled')"
                  :disabled="!isAdmin"
                  color="primary"
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
                />
              </v-col>
            </v-row>
          </v-card-text>
        </v-card>

        <v-btn v-if="isAdmin && cfg" color="primary" @click="saveConfig">
          {{ t('autosnapshot.save') }}
        </v-btn>
      </v-window-item>

      <v-window-item value="clusters">
        <AutoSnapshotClustersTab />
      </v-window-item>

      <v-window-item value="history">
        <v-card class="mb-4">
          <v-card-text>
            <v-row dense>
              <v-col cols="12" sm="4">
                <v-text-field
                  v-model="filterCluster"
                  :label="t('autosnapshot.filter.cluster')"
                  density="compact"
                  clearable
                />
              </v-col>
              <v-col cols="12" sm="4">
                <v-select
                  v-model="filterOutcome"
                  :items="outcomeOptions"
                  :label="t('autosnapshot.filter.outcome')"
                  density="compact"
                  clearable
                />
              </v-col>
              <v-col cols="12" sm="4">
                <v-select
                  v-model="filterTriggerType"
                  :items="triggerTypeOptions"
                  :label="t('autosnapshot.filter.triggerType')"
                  density="compact"
                  clearable
                />
              </v-col>
            </v-row>
          </v-card-text>
        </v-card>

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
            {{ new Date(item.CreatedAt).toLocaleString() }}
          </template>
          <template #item.TriggerType="{ item }">
            {{ t(triggerI18nKey(item.TriggerType), item.TriggerType) }}
          </template>
          <template #item.Outcome="{ item }">
            <v-chip :color="outcomeColor(item.Outcome)" size="small">
              {{ t(outcomeI18nKey(item.Outcome), item.Outcome) }}
            </v-chip>
          </template>
          <template #expanded-row="{ columns, item }">
            <tr>
              <td :colspan="columns.length" class="pa-3">
                <template v-if="item.TriggerContext && Object.keys(item.TriggerContext).length">
                  <pre class="text-caption" style="white-space: pre-wrap; margin: 0">{{ JSON.stringify(item.TriggerContext, null, 2) }}</pre>
                </template>
                <span v-else class="text-medium-emphasis">{{ t('autosnapshot.history.noContext') }}</span>
                <div v-if="item.ErrorMessage" class="mt-2 text-error">{{ item.ErrorMessage }}</div>
              </td>
            </tr>
          </template>
        </v-data-table>

        <v-pagination
          v-if="total > perPage"
          v-model="page"
          :length="Math.ceil(total / perPage)"
          class="mt-2"
        />
      </v-window-item>
    </v-window>
  </div>
</template>
