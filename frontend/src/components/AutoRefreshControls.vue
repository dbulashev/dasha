<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { formatRemaining } from '@/composables/useAutoRefresh'

const props = withDefaults(
  defineProps<{
    active: boolean
    remaining: number
    loading?: boolean
    // Seconds per tick; 0 offers the manual option, where only the button polls.
    intervalOptions?: number[]
  }>(),
  { loading: false, intervalOptions: undefined },
)

defineEmits<{ (e: 'toggle'): void; (e: 'refresh'): void }>()

const intervalSec = defineModel<number>('intervalSec', { default: 0 })

const { t } = useI18n()

const manual = computed(() => props.intervalOptions !== undefined && intervalSec.value === 0)

function optionLabel(seconds: number): string {
  return seconds === 0 ? t('autoRefresh.manual') : t('autoRefresh.intervalSec', { n: seconds })
}
</script>

<template>
  <div class="d-flex align-center flex-wrap ga-2">
    <v-btn
      :icon="active ? 'mdi-stop' : 'mdi-play'"
      :color="active ? 'error' : 'success'"
      :title="active ? t('autoRefresh.stop') : t('autoRefresh.play')"
      :disabled="manual"
      variant="tonal"
      size="small"
      @click="$emit('toggle')"
    />

    <span v-if="active" class="text-body-2 d-flex align-center ga-1">
      <v-icon size="small" color="success" class="auto-refresh-icon">mdi-refresh</v-icon>
      {{ formatRemaining(remaining) }}
    </span>

    <v-select
      v-if="intervalOptions"
      v-model="intervalSec"
      :items="intervalOptions"
      :label="t('autoRefresh.intervalLabel')"
      density="compact"
      hide-details
      style="max-width: 130px"
    >
      <template #selection="{ item }">{{ optionLabel(item.raw) }}</template>
      <template #item="{ item, props: itemProps }">
        <v-list-item v-bind="itemProps" :title="optionLabel(item.raw)" />
      </template>
    </v-select>

    <v-btn
      icon="mdi-refresh"
      :title="t('autoRefresh.refresh')"
      variant="text"
      size="small"
      :loading="loading"
      @click="$emit('refresh')"
    />
  </div>
</template>

<style scoped>
.auto-refresh-icon {
  animation: spin 2s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
