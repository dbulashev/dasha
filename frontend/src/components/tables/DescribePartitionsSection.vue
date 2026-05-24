<script setup lang="ts">
import { computed, toRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useViewError } from '@/composables/useViewError'
import { getTablesDescribePartitions } from '@/api/gen/default/default'
import type { TableDescribePartition } from '@/api/models/index'
import { useDescribeLink } from '@/composables/useDescribeLink'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { usePaginatedApiLoader } from '@/composables/useApiLoader'
import PaginationControls from '@/components/PaginationControls.vue'

const props = defineProps<{ schema: string; table: string }>()

const { t } = useI18n()
const { clusterName, hostName, databaseName } = useClusterInfo()
const { describeLink } = useDescribeLink()

const schemaRef = toRef(props, 'schema')
const tableRef = toRef(props, 'table')

const headers = computed(() => [
  { title: t('describe.partitionName'), key: 'Name' },
  { title: t('describe.partitionExpression'), key: 'PartitionExpression' },
  { title: t('header.size'), key: 'Size' },
])

const { onError } = useViewError()

const { items, loading, page, hasMore, load } = usePaginatedApiLoader<TableDescribePartition>(
  (limit, offset) => getTablesDescribePartitions({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
    schema: props.schema,
    table: props.table,
    limit,
    offset,
  }),
  {
    pageSize: 20,
    deps: [clusterName, hostName, databaseName, schemaRef, tableRef],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value && !!props.schema && !!props.table,
    onError,
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('describe.partitions') }}</v-card-title>
    <v-card-text>
      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
      >
        <template #item.Name="{ item }">
          <router-link :to="describeLink(item.Schema, item.Name)" class="text-decoration-none">{{ item.Name }}</router-link>
        </template>
        <template #item.PartitionExpression="{ value }">
          <code>{{ value }}</code>
        </template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
