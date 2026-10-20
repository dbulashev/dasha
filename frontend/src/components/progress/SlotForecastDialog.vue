<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { getReplicationWalPosition, getReplicationSlotsForecast } from '@/api/gen/default/default'
import type { ReplicationSlotsForecast, ReplicationSlotForecast } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtBytes, fmtMs } from '@/utils/format'

const open = defineModel<boolean>({ default: false })

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()

const windowOptions = [30, 60, 120, 300]
const windowSec = ref(60)

type State = 'idle' | 'measuring' | 'done' | 'error'
const state = ref<State>('idle')
const elapsedMs = ref(0)
const errorMessage = ref('')
const result = ref<ReplicationSlotsForecast | null>(null)

let timer: ReturnType<typeof setInterval> | undefined
let runId = 0

const progress = computed(() => Math.min(100, (elapsedMs.value / (windowSec.value * 1000)) * 100))
const remainingSec = computed(() => Math.max(0, Math.ceil(windowSec.value - elapsedMs.value / 1000)))

function stopTimer() {
  if (timer) clearInterval(timer)
  timer = undefined
}

function cancel() {
  runId++
  stopTimer()
  state.value = result.value ? 'done' : 'idle'
}

async function start() {
  if (!clusterName.value || !hostName.value) return
  const id = ++runId
  const params = { cluster_name: clusterName.value, instance: hostName.value }
  stopTimer()
  errorMessage.value = ''
  result.value = null
  elapsedMs.value = 0
  state.value = 'measuring'

  try {
    const pos = assertOk(await getReplicationWalPosition(params))
    if (id !== runId) return
    if (!pos.Lsn) throw new Error(t('slotForecast.noLsn'))

    const startedAt = performance.now()
    await new Promise<void>((resolve) => {
      timer = setInterval(() => {
        if (id !== runId) {
          stopTimer()
          resolve()
          return
        }
        elapsedMs.value = performance.now() - startedAt
        if (elapsedMs.value >= windowSec.value * 1000) {
          stopTimer()
          resolve()
        }
      }, 250)
    })
    if (id !== runId) return

    const fc = assertOk(await getReplicationSlotsForecast({
      ...params,
      from_lsn: pos.Lsn,
      from_time: pos.CapturedAt,
    }))
    if (id !== runId) return
    result.value = fc
    state.value = 'done'
  } catch (err) {
    if (id !== runId) return
    stopTimer()
    errorMessage.value = getErrorMessage(err)
    state.value = 'error'
  }
}

watch([clusterName, hostName], () => {
  runId++
  stopTimer()
  result.value = null
  state.value = 'idle'
})

watch(open, (v) => {
  if (!v) cancel()
})

onBeforeUnmount(() => {
  runId++
  stopTimer()
})

function fmtDuration(seconds: number): string {
  if (seconds >= 86400) {
    const d = Math.floor(seconds / 86400)
    const h = Math.floor((seconds % 86400) / 3600)
    return h > 0 ? `${d} ${t('time.d')} ${h} ${t('time.h')}` : `${d} ${t('time.d')}`
  }
  return fmtMs(seconds * 1000, t)
}

function fmtRate(bps: number | null | undefined): string {
  return bps == null ? '—' : `${fmtBytes(Math.round(bps))}/${t('time.sec')}`
}

type Verdict = { text: string; color: string }

function verdict(slot: ReplicationSlotForecast): Verdict {
  if (slot.WalStatus === 'lost') return { text: t('slotForecast.lost'), color: 'error' }
  if (slot.SafeWalSize == null) {
    return result.value?.MaxSlotWalKeepSize == null
      ? { text: t('slotForecast.unlimited'), color: 'warning' }
      : { text: '—', color: 'default' }
  }
  if (slot.SecondsToLost == null) {
    return result.value?.WalBytesPerSecond === 0
      ? { text: t('slotForecast.noLoad'), color: 'default' }
      : { text: '—', color: 'default' }
  }
  if (slot.SecondsToLost <= 0) return { text: t('slotForecast.now'), color: 'error' }
  const color = slot.SecondsToLost < 3600 ? 'error' : slot.SecondsToLost < 86400 ? 'warning' : 'success'
  return { text: fmtDuration(slot.SecondsToLost), color }
}

const headers = computed(() => [
  { title: t('replication.slotName'), key: 'SlotName' },
  { title: t('replication.slotType'), key: 'SlotType' },
  { title: t('replication.active'), key: 'Active' },
  { title: t('replication.walStatus'), key: 'WalStatus' },
  { title: t('slotForecast.retained'), key: 'RetainedBytes' },
  { title: t('replication.safeWalSize'), key: 'SafeWalSize' },
  { title: t('slotForecast.timeToLost'), key: 'SecondsToLost' },
])
</script>

<template>
  <v-dialog v-model="open" max-width="1100">
    <v-card>
      <v-card-title class="d-flex align-center ga-2">
        <v-icon icon="mdi-timer-sand" size="small" />
        {{ t('slotForecast.title') }}
        <v-spacer />
        <v-btn icon="mdi-close" variant="text" size="small" @click="open = false" />
      </v-card-title>

      <v-card-text>
        <p class="text-body-2 text-medium-emphasis mb-4">{{ t('slotForecast.description') }}</p>

        <div class="d-flex align-center ga-3 flex-wrap mb-4">
          <v-btn-toggle
            v-model="windowSec"
            mandatory
            density="compact"
            variant="outlined"
            :disabled="state === 'measuring'"
          >
            <v-btn v-for="w in windowOptions" :key="w" :value="w">{{ w }} {{ t('time.sec') }}</v-btn>
          </v-btn-toggle>
          <v-btn
            v-if="state !== 'measuring'"
            color="primary"
            prepend-icon="mdi-play"
            @click="start"
          >
            {{ result ? t('slotForecast.restart') : t('slotForecast.start') }}
          </v-btn>
          <v-btn v-else variant="tonal" prepend-icon="mdi-stop" @click="cancel">
            {{ t('slotForecast.cancel') }}
          </v-btn>
        </div>

        <template v-if="state === 'measuring'">
          <div class="text-body-2 mb-1">
            {{ t('slotForecast.measuring', { n: remainingSec }) }}
          </div>
          <v-progress-linear
            :model-value="progress"
            :indeterminate="progress >= 100"
            color="primary"
            height="8"
            rounded
            class="mb-4"
          />
        </template>

        <v-alert v-if="state === 'error'" type="error" variant="tonal" class="mb-4">
          {{ errorMessage }}
        </v-alert>

        <template v-if="result">
          <v-row dense class="mb-2">
            <v-col cols="6" md="4">
              <div class="text-caption text-medium-emphasis">{{ t('slotForecast.walRate') }}</div>
              <div class="text-body-2">{{ fmtRate(result.WalBytesPerSecond) }}</div>
            </v-col>
            <v-col cols="6" md="4">
              <div class="text-caption text-medium-emphasis">{{ t('slotForecast.window') }}</div>
              <div class="text-body-2">
                {{ result.ElapsedSeconds != null ? fmtDuration(result.ElapsedSeconds) : '—' }}
              </div>
            </v-col>
            <v-col cols="6" md="4">
              <div class="text-caption text-medium-emphasis">max_slot_wal_keep_size</div>
              <div class="text-body-2">
                {{ result.MaxSlotWalKeepSize != null ? fmtBytes(result.MaxSlotWalKeepSize) : t('slotForecast.noLimit') }}
              </div>
            </v-col>
          </v-row>

          <v-alert v-if="result.Slots.length === 0" type="info" variant="tonal">
            {{ t('replication.noSlots') }}
          </v-alert>
          <v-data-table
            v-else
            :headers="headers"
            :items="result.Slots"
            density="compact"
            :items-per-page="-1"
            hide-default-footer
          >
            <template #item.Active="{ item }">
              <v-tooltip v-if="item.Active" :text="t('slotForecast.activeHint')" location="top" max-width="420">
                <template #activator="{ props }">
                  <v-icon v-bind="props" color="success" size="small">mdi-check-circle</v-icon>
                </template>
              </v-tooltip>
              <v-icon v-else color="error" size="small">mdi-close-circle</v-icon>
            </template>
            <template #item.RetainedBytes="{ item }">{{ fmtBytes(item.RetainedBytes) }}</template>
            <template #item.SafeWalSize="{ item }">{{ fmtBytes(item.SafeWalSize) }}</template>
            <template #item.SecondsToLost="{ item }">
              <v-chip size="small" variant="tonal" :color="verdict(item).color">{{ verdict(item).text }}</v-chip>
            </template>
          </v-data-table>

          <p class="text-caption text-medium-emphasis mt-3 mb-0">{{ t('slotForecast.footnote') }}</p>
        </template>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>
