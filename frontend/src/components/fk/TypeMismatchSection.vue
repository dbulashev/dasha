<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getFkTypeMismatch } from '@/api/gen/default/default'
import type { FkTypeMismatch } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('fk.fkName'), key: 'FkName' },
  { title: t('fk.fromTable'), key: 'FromRel' },
  { title: t('fk.columns'), key: 'RelAttNames' },
  { title: t('fk.toTable'), key: 'ToRel' },
  { title: t('fk.toColumns'), key: 'ToRelAttNames' },
])
const items = ref<FkTypeMismatch[]>([])
const loading = ref(false)

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  loading.value = true
  try {
    const response = await getFkTypeMismatch({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    items.value = assertOk(response) ?? []
  } catch (err) {
    emit('error', String(err))
    items.value = []
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName, databaseName], () => load(), { immediate: true })
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('fk.typeMismatch') }}</v-card-title>
    <v-card-subtitle>{{ t('fk.typeMismatchHint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer :no-data-text="t('noData')">
        <template #item.RelAttNames="{ value }">{{ (value as string[]).join(', ') }}</template>
        <template #item.ToRelAttNames="{ value }">{{ (value as string[]).join(', ') }}</template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
