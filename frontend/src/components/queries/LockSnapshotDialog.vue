<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getSnapshotLocks } from '@/api/gen/default/default'
import type { LockSnapshot, QueryBlocked } from '@/api/models'
import { assertOk } from '@/utils/api'
import { fmtDateTime } from '@/utils/format'
import LockTree from '@/components/locks/LockTree.vue'

const props = defineProps<{ modelValue: boolean; snapshotId: string | null }>()
const emit = defineEmits<{ 'update:modelValue': [boolean] }>()

const { t } = useI18n()

const open = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const data = ref<LockSnapshot | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)

async function load() {
  if (!props.snapshotId) return
  loading.value = true
  error.value = null
  data.value = null
  try {
    data.value = assertOk<LockSnapshot>(await getSnapshotLocks(props.snapshotId))
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

// Load when the dialog opens, when mounted already open, and when the snapshot
// id changes while open.
watch(
  [open, () => props.snapshotId],
  ([isOpen]) => {
    if (isOpen) load()
  },
  { immediate: true },
)

const rows = computed<QueryBlocked[]>(() => data.value?.rows ?? [])

function fmtMsLocal(ms?: number | null): string {
  if (ms == null) return '—'
  return ms >= 1000 ? (ms / 1000).toFixed(1) + ' s' : Math.round(ms) + ' ms'
}

</script>

<template>
  <v-dialog v-model="open" max-width="1000" scrollable>
    <v-card>
      <v-card-title class="d-flex align-center ga-2">
        <v-icon icon="mdi-lock-outline" />
        {{ t('autosnapshot.locks.section') }}
        <v-spacer />
        <v-btn icon="mdi-close" variant="text" size="small" @click="open = false" />
      </v-card-title>
      <v-divider />

      <v-card-text>
        <v-progress-linear v-if="loading" indeterminate />
        <v-alert v-else-if="error" type="error" variant="tonal" density="compact">{{ error }}</v-alert>

        <template v-else-if="data">
          <div class="d-flex align-center flex-wrap ga-2 mb-3">
            <v-chip color="warning" size="small" label>
              {{ t('autosnapshot.locks.blocked') }}: {{ data.blocked_count }}
            </v-chip>
            <v-chip v-if="data.max_wait_ms" size="small" label variant="tonal">
              {{ t('autosnapshot.locks.maxWait') }}: {{ fmtMsLocal(data.max_wait_ms) }}
            </v-chip>
            <span
              v-if="data.background_peak && data.background_peak.blocked_count > data.blocked_count"
              class="text-caption text-medium-emphasis"
            >
              {{ t('autosnapshot.locks.peakWas', { count: data.background_peak.blocked_count, at: fmtDateTime(data.background_peak.at, '') }) }}
            </span>
          </div>

          <LockTree v-if="rows.length" :items="rows" />
          <div v-else class="text-medium-emphasis">{{ t('autosnapshot.locks.none') }}</div>
        </template>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>
