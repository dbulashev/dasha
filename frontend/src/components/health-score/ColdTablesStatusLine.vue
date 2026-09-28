<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ColdTablesStatus } from '@/api/models/index'

const props = defineProps<{ status?: ColdTablesStatus | null }>()

const { t } = useI18n()

const visible = computed(() => {
  const s = props.status?.status
  if (!s || s === 'standby' || s === 'no_storage') return false
  return s !== 'available' || (props.status?.count ?? 0) > 0
})

const available = computed(() => props.status?.status === 'available')

const text = computed(() =>
  available.value
    ? t('healthScore.cold.count', { n: props.status?.count ?? 0, days: props.status?.window_days ?? 0 })
    : t('healthScore.cold.unavailable'),
)

const hint = computed(() => {
  const s = props.status
  if (!s) return ''
  if (s.status === 'available') return t('healthScore.cold.countHint')
  return t(`healthScore.cold.reason.${s.status}`, { observed: s.observed_days ?? 0, days: s.window_days })
})
</script>

<template>
  <div v-if="visible" class="d-flex align-center ga-1 text-caption text-medium-emphasis">
    <v-icon size="small" :icon="available ? 'mdi-snowflake' : 'mdi-snowflake-off'" />
    <span>{{ text }}</span>
    <v-tooltip :text="hint" location="bottom" max-width="400">
      <template #activator="{ props: tp }">
        <v-icon v-bind="tp" size="x-small">mdi-help-circle-outline</v-icon>
      </template>
    </v-tooltip>
  </div>
</template>
