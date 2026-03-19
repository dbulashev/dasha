<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAutovacuumSettings } from '@/api/gen/default/default'
import type { PgSetting } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('settings.name'), key: 'Name' },
  { title: t('settings.value'), key: 'SettingWithUnit' },
  { title: t('settings.source'), key: 'Source' },
])
const items = ref<PgSetting[]>([])
const loading = ref(false)

const displayItems = computed(() =>
  items.value.map((s) => ({
    ...s,
    SettingWithUnit: s.Unit ? `${s.Setting} ${s.Unit}` : s.Setting,
  })),
)

async function load() {
  if (!clusterName.value || !hostName.value) return
  loading.value = true
  try {
    const response = await getAutovacuumSettings({
      cluster_name: clusterName.value,
      instance: hostName.value,
    })
    items.value = assertOk(response) ?? []
  } catch (err) {
    emit('error', String(err))
    items.value = []
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName], () => load(), { immediate: true })
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('settings.autovacuumSettings') }}</v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="displayItems" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer :no-data-text="t('noData')" />
    </v-card-text>
  </v-card>
</template>
