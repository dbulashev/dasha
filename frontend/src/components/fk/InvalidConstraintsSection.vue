<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getInvalidConstraints } from '@/api/gen/default/default'
import type { InvalidConstraint } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.schema'), key: 'Schema' },
  { title: t('header.table'), key: 'Table' },
  { title: t('header.constraint'), key: 'Name' },
  { title: t('fk.referencedSchema'), key: 'ReferencedSchema' },
  { title: t('fk.referencedTable'), key: 'ReferencedTable' },
])
const items = ref<InvalidConstraint[]>([])
const loading = ref(false)

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  loading.value = true
  try {
    const response = await getInvalidConstraints({
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
    <v-card-title>{{ t('fk.invalidConstraints') }}</v-card-title>
    <v-card-subtitle>{{ t('fk.invalidConstraintsHint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading" density="compact" multi-sort :items-per-page="-1" hide-default-footer :no-data-text="t('fk.noInvalidConstraints')" />
    </v-card-text>
  </v-card>
</template>
