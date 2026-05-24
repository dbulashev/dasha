<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getTablesPartitions } from '@/api/gen/default/default'
import type { TablePartition } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import { useDescribeLink } from '@/composables/useDescribeLink'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { describeLink } = useDescribeLink()
const { t } = useI18n()
const { onError } = useViewError()

const headers = computed(() => [
  { title: t('header.parentSchema'), key: 'ParentSchema' },
  { title: t('header.parent'), key: 'Parent' },
  { title: t('header.childsCount'), key: 'ChildsCount' },
  { title: t('header.childsSize'), key: 'ChildsSizeBytes' },
  { title: t('header.childsAvgSize'), key: 'ChildsAvgSizeBytes' },
])

const { items, loading } = useApiLoader<TablePartition[]>(
  () => getTablesPartitions({
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
      <v-icon start icon="mdi-view-grid-outline" />{{ t('tables.partitions') }}
      <v-tooltip :text="t('hint.partitions')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading">
        <template #item.Parent="{ item }">
          <router-link :to="describeLink(item.ParentSchema, item.Parent)" class="text-decoration-none">{{ item.Parent }}</router-link>
        </template>
        <template #item.ChildsSizeBytes="{ item }">{{ item.ChildsSize }}</template>
        <template #item.ChildsAvgSizeBytes="{ item }">{{ item.ChildsAvgSize }}</template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
