<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAutovacuumSettings } from '@/api/gen/default/default'
import type { PgSetting } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('settings.name'), key: 'Name' },
  { title: t('settings.value'), key: 'SettingWithUnit' },
  { title: t('settings.source'), key: 'Source' },
])

const { items, loading } = useApiLoader<PgSetting[]>(
  () => getAutovacuumSettings({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError: (msg) => emit('error', msg),
  },
)

const displayItems = computed(() =>
  items.value.map((s) => ({
    ...s,
    SettingWithUnit: s.Unit ? `${s.Setting} ${s.Unit}` : s.Setting,
  })),
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('settings.autovacuumSettings') }}</v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="displayItems" :loading="loading" />
    </v-card-text>
  </v-card>
</template>
