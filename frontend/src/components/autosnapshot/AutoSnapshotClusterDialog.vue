<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getAutosnapshotCluster,
  putAutosnapshotCluster,
} from '@/api/gen/default/default'
import type {
  AutoSnapshotClusterOverride,
  AutoSnapshotClusterOverrideInput,
} from '@/api/models'
import { AuthInfoMode } from '@/api/models'
import { useAuthStore } from '@/stores/auth'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

const props = defineProps<{
  modelValue: boolean
  clusterName: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: [clusterName: string]
}>()

const { t } = useI18n()
const authStore = useAuthStore()

const isAdmin = computed(
  () => authStore.mode === AuthInfoMode.none || authStore.user?.role === 'admin',
)

const loading = ref(false)
const saving = ref(false)
const error = ref<string | null>(null)
const data = ref<AutoSnapshotClusterOverride | null>(null)
const formRef = ref()

// Validators — empty means "use the global default" (the field is clearable), so an
// empty value is always valid. Bounds mirror the backend (validateClusterOverrides).
function isEmpty(v: unknown): boolean {
  return v === null || v === undefined || v === ''
}
const durationUnitMs: Record<string, number> = {
  ns: 1e-6, us: 1e-3, 'µs': 1e-3, ms: 1, s: 1000, m: 60000, h: 3600000,
}
function goDurationToMs(v: string): number | null {
  const str = (v ?? '').trim()
  if (!/^(\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h))+$/.test(str)) return null
  const re = /(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g
  let total = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(str)) !== null) {
    total += parseFloat(m[1]) * durationUnitMs[m[2]]
  }
  return total
}
const durationRule = (v: string | null) => {
  if (isEmpty(v)) return true
  const ms = goDurationToMs(String(v))
  return (ms !== null && ms > 0) || t('autosnapshot.invalidDuration')
}
const thresholdRule = (v: number | null) =>
  isEmpty(v) ||
  (Number.isInteger(Number(v)) && Number(v) > 0 && Number(v) <= 10000) ||
  t('autosnapshot.clusters.thresholdRange')

type ActivitySpikeForm = {
  Enabled: boolean | null
  WindowSize: string | null
  ActiveThresholdPct: number | null
  SpikeDuration: string | null
  RecoveryDuration: string | null
  DeferredInterval: string | null
}

// Allows empty (use default) or a valid duration >= 0 (incl. "0s" to disable).
const durationGteZeroRule = (v: string | null) => {
  if (isEmpty(v)) return true
  const ms = goDurationToMs(String(v))
  return (ms !== null && ms >= 0) || t('autosnapshot.invalidDuration')
}
type RoleChangeForm = {
  Enabled: boolean | null
  Direction: string | null
}

const form = reactive<{
  activitySpike: ActivitySpikeForm
  roleChange: RoleChangeForm
}>({
  activitySpike: {
    Enabled: null,
    WindowSize: null,
    ActiveThresholdPct: null,
    SpikeDuration: null,
    RecoveryDuration: null,
    DeferredInterval: null,
  },
  roleChange: {
    Enabled: null,
    Direction: null,
  },
})

const directionOptions = computed(() => [
  { value: 'both', title: t('autosnapshot.direction.both') },
  { value: 'master_to_replica', title: t('autosnapshot.direction.masterToReplica') },
  { value: 'replica_to_master', title: t('autosnapshot.direction.replicaToMaster') },
])

// Human label for an effective direction value (used as the placeholder).
function directionLabel(v: string | null | undefined): string {
  if (!v) return ''
  return directionOptions.value.find((o) => o.value === v)?.title ?? v
}

// True when the cluster has at least one field overriding the global default.
const hasAnyOverride = computed(
  () =>
    form.activitySpike.Enabled !== null ||
    form.activitySpike.WindowSize !== null ||
    form.activitySpike.ActiveThresholdPct !== null ||
    form.activitySpike.SpikeDuration !== null ||
    form.activitySpike.RecoveryDuration !== null ||
    form.activitySpike.DeferredInterval !== null ||
    form.roleChange.Enabled !== null ||
    form.roleChange.Direction !== null,
)

type OverridesShape = {
  activity_spike?: Partial<{
    enabled: boolean
    window_size: string
    active_threshold_pct: number
    spike_duration: string
    recovery_duration: string
    deferred_interval: string
  }>
  role_change?: Partial<{
    enabled: boolean
    direction: string
  }>
}

function resetForm() {
  form.activitySpike.Enabled = null
  form.activitySpike.WindowSize = null
  form.activitySpike.ActiveThresholdPct = null
  form.activitySpike.SpikeDuration = null
  form.activitySpike.RecoveryDuration = null
  form.activitySpike.DeferredInterval = null
  form.roleChange.Enabled = null
  form.roleChange.Direction = null
}

function fillFormFromOverrides(overrides: unknown) {
  resetForm()
  if (!overrides || typeof overrides !== 'object') return
  const o = overrides as OverridesShape
  if (o.activity_spike) {
    if (typeof o.activity_spike.enabled === 'boolean') form.activitySpike.Enabled = o.activity_spike.enabled
    if (typeof o.activity_spike.window_size === 'string') form.activitySpike.WindowSize = o.activity_spike.window_size
    if (typeof o.activity_spike.active_threshold_pct === 'number') form.activitySpike.ActiveThresholdPct = o.activity_spike.active_threshold_pct
    if (typeof o.activity_spike.spike_duration === 'string') form.activitySpike.SpikeDuration = o.activity_spike.spike_duration
    if (typeof o.activity_spike.recovery_duration === 'string') form.activitySpike.RecoveryDuration = o.activity_spike.recovery_duration
    if (typeof o.activity_spike.deferred_interval === 'string') form.activitySpike.DeferredInterval = o.activity_spike.deferred_interval
  }
  if (o.role_change) {
    if (typeof o.role_change.enabled === 'boolean') form.roleChange.Enabled = o.role_change.enabled
    if (typeof o.role_change.direction === 'string') form.roleChange.Direction = o.role_change.direction
  }
}

function buildOverrides(): Record<string, unknown> {
  const out: OverridesShape = {}
  const as: OverridesShape['activity_spike'] = {}
  if (form.activitySpike.Enabled !== null) as.enabled = form.activitySpike.Enabled
  if (form.activitySpike.WindowSize !== null) as.window_size = form.activitySpike.WindowSize
  if (form.activitySpike.ActiveThresholdPct !== null) as.active_threshold_pct = form.activitySpike.ActiveThresholdPct
  if (form.activitySpike.SpikeDuration !== null) as.spike_duration = form.activitySpike.SpikeDuration
  if (form.activitySpike.RecoveryDuration !== null) as.recovery_duration = form.activitySpike.RecoveryDuration
  if (form.activitySpike.DeferredInterval !== null) as.deferred_interval = form.activitySpike.DeferredInterval
  if (Object.keys(as).length) out.activity_spike = as

  const rc: OverridesShape['role_change'] = {}
  if (form.roleChange.Enabled !== null) rc.enabled = form.roleChange.Enabled
  if (form.roleChange.Direction !== null) rc.direction = form.roleChange.Direction
  if (Object.keys(rc).length) out.role_change = rc

  return out as Record<string, unknown>
}

async function loadOverride() {
  loading.value = true
  error.value = null
  try {
    const res = await getAutosnapshotCluster(props.clusterName)
    const body = assertOk<AutoSnapshotClusterOverride>(res)
    data.value = body
    fillFormFromOverrides(body?.Overrides)
  } catch (e) {
    error.value = t('autosnapshot.clusters.loadError') + ': ' + getErrorMessage(e)
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!data.value) return
  if (formRef.value) {
    const { valid } = await formRef.value.validate()
    if (!valid) return
  }
  saving.value = true
  error.value = null
  try {
    const payload: AutoSnapshotClusterOverrideInput = {
      Overrides: buildOverrides(),
    }
    const res = await putAutosnapshotCluster(props.clusterName, payload)
    assertOk(res)
    emit('saved', props.clusterName)
    close(false)
  } catch (e) {
    error.value = getErrorMessage(e)
  } finally {
    saving.value = false
  }
}

function close(value: boolean) {
  emit('update:modelValue', value)
}

function clearField<K extends keyof ActivitySpikeForm>(group: 'activitySpike', key: K): void
function clearField<K extends keyof RoleChangeForm>(group: 'roleChange', key: K): void
function clearField(group: 'activitySpike' | 'roleChange', key: string) {
  const g = form[group] as Record<string, unknown>
  g[key] = null
}

function isOverridden(group: 'activitySpike' | 'roleChange', key: string): boolean {
  const g = form[group] as Record<string, unknown>
  return g[key] !== null
}

function onSwitchChange(
  group: 'activitySpike' | 'roleChange',
  effectiveValue: boolean,
  value: boolean,
) {
  const g = form[group] as Record<string, unknown>
  g.Enabled = value === effectiveValue ? null : value
}

onMounted(loadOverride)
</script>

<template>
  <v-dialog
    :model-value="props.modelValue"
    max-width="720"
    @update:model-value="close($event)"
  >
    <v-card>
      <v-card-title>
        {{ t('autosnapshot.clusters.editTitle', { name: props.clusterName }) }}
      </v-card-title>

      <v-card-text>
        <v-progress-linear v-if="loading" indeterminate />
        <v-alert v-if="error" type="error" class="mb-4">{{ error }}</v-alert>

        <v-form v-if="data" ref="formRef">
          <!-- Activity spike -->
          <div class="text-subtitle-1 mb-2">{{ t('autosnapshot.activitySpike') }}</div>
          <v-row dense>
            <v-col cols="12" sm="6">
              <div class="d-flex align-center">
                <v-switch
                  :model-value="form.activitySpike.Enabled ?? data.Effective.ActivitySpike.Enabled"
                  :label="t('autosnapshot.enabled')"
                  :disabled="!isAdmin"
                  color="primary"
                  hide-details
                  @update:model-value="onSwitchChange('activitySpike', data.Effective.ActivitySpike.Enabled, $event as boolean)"
                />
                <v-icon
                  v-if="isOverridden('activitySpike', 'Enabled')"
                  size="x-small"
                  color="primary"
                  class="ml-2"
                  v-tooltip="t('autosnapshot.overridden')"
                >
                  mdi-circle-medium
                </v-icon>
                <v-btn
                  v-if="isOverridden('activitySpike', 'Enabled') && isAdmin"
                  size="x-small"
                  variant="text"
                  icon="mdi-close"
                  v-tooltip="t('autosnapshot.clusters.useDefault')"
                  @click="clearField('activitySpike', 'Enabled')"
                />
              </div>
            </v-col>
            <v-col cols="12" sm="6">
              <v-text-field
                v-model="form.activitySpike.WindowSize"
                :label="t('autosnapshot.windowSize')"
                :placeholder="data.Effective.ActivitySpike.WindowSize"
                :disabled="!isAdmin"
                :rules="[durationRule]"
                density="compact"
                persistent-placeholder
                clearable
              >
                <template #append-inner>
                  <v-icon
                    v-if="isOverridden('activitySpike', 'WindowSize')"
                    size="x-small"
                    color="primary"
                    v-tooltip="t('autosnapshot.overridden')"
                  >
                    mdi-circle-medium
                  </v-icon>
                </template>
              </v-text-field>
            </v-col>
            <v-col cols="12" sm="6">
              <v-text-field
                v-model.number="form.activitySpike.ActiveThresholdPct"
                :label="t('autosnapshot.thresholdPct')"
                :placeholder="String(data.Effective.ActivitySpike.ActiveThresholdPct)"
                :disabled="!isAdmin"
                :rules="[thresholdRule]"
                type="number"
                density="compact"
                persistent-placeholder
                clearable
              >
                <template #append-inner>
                  <v-icon
                    v-if="isOverridden('activitySpike', 'ActiveThresholdPct')"
                    size="x-small"
                    color="primary"
                    v-tooltip="t('autosnapshot.overridden')"
                  >
                    mdi-circle-medium
                  </v-icon>
                </template>
              </v-text-field>
            </v-col>
            <v-col cols="12" sm="6">
              <v-text-field
                v-model="form.activitySpike.SpikeDuration"
                :label="t('autosnapshot.spikeDuration')"
                :placeholder="data.Effective.ActivitySpike.SpikeDuration"
                :disabled="!isAdmin"
                :rules="[durationRule]"
                density="compact"
                persistent-placeholder
                clearable
              >
                <template #append-inner>
                  <v-icon
                    v-if="isOverridden('activitySpike', 'SpikeDuration')"
                    size="x-small"
                    color="primary"
                    v-tooltip="t('autosnapshot.overridden')"
                  >
                    mdi-circle-medium
                  </v-icon>
                </template>
              </v-text-field>
            </v-col>
            <v-col cols="12" sm="6">
              <v-text-field
                v-model="form.activitySpike.RecoveryDuration"
                :label="t('autosnapshot.recoveryDuration')"
                :placeholder="data.Effective.ActivitySpike.RecoveryDuration"
                :disabled="!isAdmin"
                :rules="[durationGteZeroRule]"
                density="compact"
                persistent-placeholder
                clearable
              >
                <template #append-inner>
                  <v-icon
                    v-if="isOverridden('activitySpike', 'RecoveryDuration')"
                    size="x-small"
                    color="primary"
                    v-tooltip="t('autosnapshot.overridden')"
                  >
                    mdi-circle-medium
                  </v-icon>
                </template>
              </v-text-field>
            </v-col>
            <v-col cols="12" sm="6">
              <v-text-field
                v-model="form.activitySpike.DeferredInterval"
                :label="t('autosnapshot.deferredInterval')"
                :placeholder="data.Effective.ActivitySpike.DeferredInterval"
                :disabled="!isAdmin"
                :rules="[durationGteZeroRule]"
                density="compact"
                persistent-placeholder
                clearable
              >
                <template #append-inner>
                  <v-icon
                    v-if="isOverridden('activitySpike', 'DeferredInterval')"
                    size="x-small"
                    color="primary"
                    v-tooltip="t('autosnapshot.overridden')"
                  >
                    mdi-circle-medium
                  </v-icon>
                </template>
              </v-text-field>
            </v-col>
          </v-row>

          <v-divider class="my-4" />

          <!-- Role change -->
          <div class="text-subtitle-1 mb-2">{{ t('autosnapshot.roleChange') }}</div>
          <v-row dense>
            <v-col cols="12" sm="6">
              <div class="d-flex align-center">
                <v-switch
                  :model-value="form.roleChange.Enabled ?? data.Effective.RoleChange.Enabled"
                  :label="t('autosnapshot.enabled')"
                  :disabled="!isAdmin"
                  color="primary"
                  hide-details
                  @update:model-value="onSwitchChange('roleChange', data.Effective.RoleChange.Enabled, $event as boolean)"
                />
                <v-icon
                  v-if="isOverridden('roleChange', 'Enabled')"
                  size="x-small"
                  color="primary"
                  class="ml-2"
                  v-tooltip="t('autosnapshot.overridden')"
                >
                  mdi-circle-medium
                </v-icon>
                <v-btn
                  v-if="isOverridden('roleChange', 'Enabled') && isAdmin"
                  size="x-small"
                  variant="text"
                  icon="mdi-close"
                  v-tooltip="t('autosnapshot.clusters.useDefault')"
                  @click="clearField('roleChange', 'Enabled')"
                />
              </div>
            </v-col>
            <v-col cols="12" sm="6">
              <v-select
                v-model="form.roleChange.Direction"
                :items="directionOptions"
                :label="t('autosnapshot.direction.label')"
                :placeholder="directionLabel(data.Effective.RoleChange.Direction)"
                :disabled="!isAdmin"
                density="compact"
                persistent-placeholder
                clearable
              >
                <template #append-inner>
                  <v-icon
                    v-if="isOverridden('roleChange', 'Direction')"
                    size="x-small"
                    color="primary"
                    v-tooltip="t('autosnapshot.overridden')"
                  >
                    mdi-circle-medium
                  </v-icon>
                </template>
              </v-select>
            </v-col>
          </v-row>
        </v-form>
      </v-card-text>

      <v-card-actions>
        <v-btn
          v-if="isAdmin"
          variant="text"
          prepend-icon="mdi-restore"
          :disabled="!hasAnyOverride"
          @click="resetForm"
        >
          {{ t('autosnapshot.clusters.resetToDefault') }}
        </v-btn>
        <v-spacer />
        <v-btn variant="text" @click="close(false)">{{ t('autosnapshot.clusters.close') }}</v-btn>
        <v-btn
          v-if="isAdmin"
          color="primary"
          :loading="saving"
          :disabled="loading"
          @click="save"
        >
          {{ t('autosnapshot.save') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
