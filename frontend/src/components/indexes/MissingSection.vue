<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIndexesMissing } from '@/api/gen/default/default'
import type { IndexMissing } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import { useDescribeLink } from '@/composables/useDescribeLink'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { describeLink } = useDescribeLink()
const { t } = useI18n()
const { onError } = useViewError()

const headers = computed(() => [
  { title: t('header.schema'), key: 'Schema' },
  { title: t('header.table'), key: 'Table' },
  { title: t('header.percentIndexUsed'), key: 'PercentOfTimesIndexUsed' },
  { title: t('header.estimatedRows'), key: 'EstimatedRows' },
])

const { items, loading } = useApiLoader<IndexMissing[]>(
  () => getIndexesMissing({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
  }),
  {
    deps: [clusterName, hostName, databaseName],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError,
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      {{ t('indexes.missing') }}
      <v-tooltip :text="t('hint.missingIndexes')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading">
        <template #item.Table="{ item }">
          <router-link :to="describeLink(item.Schema, item.Table)" class="text-decoration-none">{{ item.Table }}</router-link>
        </template>
        <template #item.PercentOfTimesIndexUsed="{ value }">{{ value != null ? value + '%' : t('insufficientData') }}</template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
