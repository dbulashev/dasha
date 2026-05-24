<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { storeToRefs } from 'pinia'
import {
  getHealthScoreWeights,
  putHealthScoreWeights,
  deleteHealthScoreWeights,
} from '@/api/gen/default/default'
import { AuthInfoMode } from '@/api/models'
import type { HealthScoreWeights } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import { useAuthStore } from '@/stores/auth'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

type WeightKey = 'connections' | 'performance' | 'storage' | 'replication' | 'maintenance'

const CATEGORIES: WeightKey[] = ['connections', 'performance', 'storage', 'replication', 'maintenance']
const TOTAL = 100

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()
const authStore = useAuthStore()
const { mode, user } = storeToRefs(authStore)

const canEdit = computed(() => mode.value === AuthInfoMode.none || user.value?.role === 'admin')

const { items: data, loading, load } = useApiLoader<HealthScoreWeights | null>(
  () =>
    getHealthScoreWeights({
      cluster_name: clusterName.value!,
    }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value,
    onError,
    defaultValue: null,
  },
)

const draft = ref<Record<WeightKey, number>>({
  connections: 0,
  performance: 0,
  storage: 0,
  replication: 0,
  maintenance: 0,
})

const saving = ref(false)
const resetting = ref(false)
const dirty = ref(false)

watch(
  data,
  (d) => {
    if (!d) return
    // Backend stores normalized 0..1 values; UI works in 0..100.
    let used = 0
    for (let i = 0; i < CATEGORIES.length - 1; i++) {
      const key = CATEGORIES[i]
      const v = Math.round(d[key] * TOTAL)
      draft.value[key] = v
      used += v
    }
    // Last category gets the remainder so the sum is exactly TOTAL.
    draft.value[CATEGORIES[CATEGORIES.length - 1]] = Math.max(0, TOTAL - used)
    dirty.value = false
  },
  { immediate: true },
)

// adjustSlider keeps the total at TOTAL by redistributing the delta across
// the other sliders proportionally to their current values. When all other
// sliders are zero, the remainder is split evenly.
function adjustSlider(changedKey: WeightKey, rawValue: number) {
  const newValue = Math.min(TOTAL, Math.max(0, Math.round(rawValue)))
  const others = CATEGORIES.filter((k) => k !== changedKey)
  const othersSum = others.reduce((acc, k) => acc + draft.value[k], 0)

  draft.value[changedKey] = newValue
  const remaining = TOTAL - newValue

  if (othersSum > 0) {
    let leftToDistribute = remaining
    // Distribute to all but the last proportionally, last gets the remainder
    // to guarantee an exact sum.
    for (let i = 0; i < others.length - 1; i++) {
      const k = others[i]
      const share = Math.round((draft.value[k] / othersSum) * remaining)
      draft.value[k] = Math.max(0, share)
      leftToDistribute -= draft.value[k]
    }
    draft.value[others[others.length - 1]] = Math.max(0, leftToDistribute)
  } else if (others.length > 0) {
    const perOther = Math.floor(remaining / others.length)
    let leftover = remaining - perOther * others.length
    for (const k of others) {
      draft.value[k] = perOther
    }
    draft.value[others[0]] += leftover
  }

  dirty.value = true
}

async function save() {
  if (!clusterName.value) return
  saving.value = true
  try {
    const res = await putHealthScoreWeights(
      {
        connections: draft.value.connections,
        performance: draft.value.performance,
        storage: draft.value.storage,
        replication: draft.value.replication,
        maintenance: draft.value.maintenance,
      },
      { cluster_name: clusterName.value },
    )
    assertOk(res)
    await load()
  } catch (err) {
    onError(getErrorMessage(err), err)
  } finally {
    saving.value = false
  }
}

async function reset() {
  if (!clusterName.value) return
  resetting.value = true
  try {
    const res = await deleteHealthScoreWeights({ cluster_name: clusterName.value })
    assertOk(res)
    await load()
  } catch (err) {
    onError(getErrorMessage(err), err)
  } finally {
    resetting.value = false
  }
}

function categoryLabel(name: string): string {
  return t(`healthScore.categories.${name}`)
}
</script>

<template>
  <v-card>
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-tune" />
      {{ t('healthScore.weights.sectionTitle') }}
      <v-tooltip :text="t('healthScore.weights.tooltip')" location="bottom">
        <template #activator="{ props: tp }">
          <v-icon v-bind="tp" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
      <v-spacer />
      <v-chip v-if="data" size="small" :color="data.source === 'override' ? 'primary' : 'default'" variant="tonal">
        {{ t(`healthScore.weights.source.${data.source}`) }}
      </v-chip>
    </v-card-title>
    <v-card-text>
      <v-alert
        v-if="!canEdit"
        type="info"
        variant="tonal"
        density="compact"
        class="mb-3"
        icon="mdi-information-outline"
      >
        {{ t('healthScore.weights.readonlyHint') }}
      </v-alert>

      <v-skeleton-loader v-if="loading" type="text@5" />
      <template v-else>
        <div
          v-for="key in CATEGORIES"
          :key="key"
          class="d-flex align-center ga-3 mb-2"
        >
          <div class="weights-label">{{ categoryLabel(key) }}</div>
          <v-slider
            :model-value="draft[key]"
            :min="0"
            :max="100"
            :step="1"
            :disabled="!canEdit"
            density="compact"
            hide-details
            color="primary"
            class="flex-grow-1"
            @update:model-value="(v) => adjustSlider(key, v)"
          />
          <div class="weights-value text-right">{{ draft[key] }}%</div>
        </div>

        <div v-if="canEdit" class="d-flex ga-2 mt-3">
          <v-btn
            color="primary"
            :loading="saving"
            :disabled="!dirty"
            @click="save"
          >
            {{ t('healthScore.weights.saveBtn') }}
          </v-btn>
          <v-btn
            variant="text"
            :loading="resetting"
            :disabled="data?.source !== 'override'"
            @click="reset"
          >
            {{ t('healthScore.weights.resetBtn') }}
          </v-btn>
        </div>

        <div v-if="data?.updated_at" class="text-caption text-medium-emphasis mt-3">
          {{ t('healthScore.weights.updatedAt', { ts: new Date(data.updated_at).toLocaleString() }) }}
        </div>
      </template>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.weights-label {
  min-width: 160px;
}
.weights-value {
  min-width: 64px;
  font-variant-numeric: tabular-nums;
}
</style>
