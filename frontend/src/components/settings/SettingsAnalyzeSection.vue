<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getSettingsAnalyze } from '@/api/gen/default/default'
import type { SettingsNotification } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const items = ref<SettingsNotification[]>([])
const loading = ref(false)

async function load() {
  if (!clusterName.value || !hostName.value) return
  loading.value = true
  try {
    const response = await getSettingsAnalyze({
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
    <v-card-title>{{ t('settings.analyze') }}</v-card-title>
    <v-card-text>
      <v-progress-linear v-if="loading" indeterminate />
      <v-list v-else-if="items.length" density="compact" class="pa-0">
        <v-list-item v-for="(item, idx) in items" :key="idx">
          <v-list-item-title class="text-body-2">{{ t('settings.n.' + item.Key, item.Params) }}</v-list-item-title>
        </v-list-item>
      </v-list>
      <div v-else class="text-success text-body-2 pa-1">
        {{ t('settings.noIssues') }}
      </div>
    </v-card-text>
  </v-card>
</template>
