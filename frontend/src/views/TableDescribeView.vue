<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getTablesDescribe } from '@/api/gen/default/default'
import type { TableDescribe } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { clusterName, databaseName, hostName } = useClusterInfo()

const schema = computed(() => route.query.schema ? String(route.query.schema) : '')
const table = computed(() => route.query.table ? String(route.query.table) : '')

const data = ref<TableDescribe | null>(null)
const loading = ref(false)
const errorMessage = ref('')

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value || !schema.value || !table.value) return
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getTablesDescribe({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      schema: schema.value,
      table: table.value,
    })
    data.value = assertOk(response) as TableDescribe
  } catch (err) {
    errorMessage.value = getErrorMessage(err)
    data.value = null
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName, databaseName, schema, table], () => load(), { immediate: true })

const title = computed(() => {
  if (!data.value) return ''
  return `${data.value.Schema}.${data.value.TableName}`
})

const tableTypeLabel = computed(() => {
  if (!data.value) return ''
  switch (data.value.TableType) {
    case 'partitioned_table': return t('describe.partitionedTable')
    case 'table': return t('describe.table')
    default: return data.value.TableType
  }
})

const columnHeaders = computed(() => [
  { title: t('describe.columnName'), key: 'Name' },
  { title: t('describe.type'), key: 'Type' },
  { title: t('describe.collation'), key: 'Collation' },
  { title: t('describe.nullable'), key: 'Nullable' },
  { title: t('describe.default'), key: 'Default' },
  { title: t('describe.storage'), key: 'Storage' },
  { title: t('describe.description'), key: 'Description' },
])

const indexHeaders = computed(() => [
  { title: t('describe.indexName'), key: 'Name' },
  { title: t('header.definition'), key: 'Definition' },
  { title: t('header.primary'), key: 'IsPrimary' },
  { title: t('header.unique'), key: 'IsUnique' },
])

const constraintHeaders = computed(() => [
  { title: t('describe.constraintName'), key: 'Name' },
  { title: t('header.definition'), key: 'Definition' },
])

const referencedByHeaders = computed(() => [
  { title: t('describe.constraintName'), key: 'Name' },
  { title: t('describe.sourceTable'), key: 'SourceTable' },
  { title: t('header.definition'), key: 'Definition' },
])

function goBack() {
  router.back()
}
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>

  <v-alert v-if="!schema || !table" type="info" class="mb-4">
    {{ t('describe.selectTable') }}
  </v-alert>

  <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-4" />

  <template v-if="data">
    <!-- Header -->
    <v-card class="mb-4">
      <v-card-title class="d-flex align-center ga-2">
        <v-btn icon="mdi-arrow-left" variant="text" size="small" @click="goBack" />
        <span class="text-h6 font-weight-bold">{{ title }}</span>
        <v-chip size="small" color="primary" variant="tonal">{{ tableTypeLabel }}</v-chip>
        <v-chip v-if="data.AccessMethod" size="small" variant="outlined">{{ data.AccessMethod }}</v-chip>
        <v-chip v-if="data.Tablespace" size="small" variant="outlined" prepend-icon="mdi-harddisk">{{ data.Tablespace }}</v-chip>
      </v-card-title>

      <v-card-text v-if="data.PartitionOf" class="pt-0">
        <v-chip size="small" color="secondary" variant="tonal" prepend-icon="mdi-file-tree">
          {{ t('describe.partitionOf') }}: {{ data.PartitionOf }}
        </v-chip>
      </v-card-text>

      <v-card-text v-if="data.Options" class="pt-0">
        <v-chip size="small" variant="outlined" prepend-icon="mdi-cog-outline">{{ data.Options }}</v-chip>
      </v-card-text>
    </v-card>

    <!-- Size -->
    <v-card class="mb-4">
      <v-card-title>{{ t('describe.size') }}</v-card-title>
      <v-card-text>
        <v-row>
          <v-col cols="6" sm="3">
            <div class="text-caption text-medium-emphasis">{{ t('describe.sizeTotal') }}</div>
            <div class="text-h6">{{ data.SizeTotal }}</div>
          </v-col>
          <v-col cols="6" sm="3">
            <div class="text-caption text-medium-emphasis">{{ t('describe.sizeTable') }}</div>
            <div class="text-h6">{{ data.SizeTable }}</div>
          </v-col>
          <v-col cols="6" sm="3">
            <div class="text-caption text-medium-emphasis">{{ t('describe.sizeToast') }}</div>
            <div class="text-h6">{{ data.SizeToast }}</div>
          </v-col>
          <v-col cols="6" sm="3">
            <div class="text-caption text-medium-emphasis">{{ t('describe.sizeIndexes') }}</div>
            <div class="text-h6">{{ data.SizeIndexes }}</div>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <!-- Columns -->
    <v-card class="mb-4">
      <v-card-title>{{ t('describe.columns') }} ({{ data.Columns.length }})</v-card-title>
      <v-card-text>
        <v-data-table
          :headers="columnHeaders"
          :items="data.Columns"
        >
          <template #item.Nullable="{ value }">
            <v-icon v-if="value" icon="mdi-check" color="success" size="small" />
          </template>
          <template #item.Default="{ value }">
            <code v-if="value">{{ value }}</code>
          </template>
        </v-data-table>
      </v-card-text>
    </v-card>

    <!-- Indexes -->
    <v-card v-if="data.Indexes.length" class="mb-4">
      <v-card-title>{{ t('describe.indexes') }} ({{ data.Indexes.length }})</v-card-title>
      <v-card-text>
        <v-data-table
          :headers="indexHeaders"
          :items="data.Indexes"
        >
          <template #item.IsPrimary="{ value }">
            <v-icon v-if="value" icon="mdi-key" color="warning" size="small" />
          </template>
          <template #item.IsUnique="{ value }">
            <v-icon v-if="value" icon="mdi-check" color="success" size="small" />
          </template>
          <template #item.Definition="{ value }">
            <code>{{ value }}</code>
          </template>
        </v-data-table>
      </v-card-text>
    </v-card>

    <!-- Check Constraints -->
    <v-card v-if="data.CheckConstraints.length" class="mb-4">
      <v-card-title>{{ t('describe.checkConstraints') }} ({{ data.CheckConstraints.length }})</v-card-title>
      <v-card-text>
        <v-data-table
          :headers="constraintHeaders"
          :items="data.CheckConstraints"
        >
          <template #item.Definition="{ value }">
            <code>{{ value }}</code>
          </template>
        </v-data-table>
      </v-card-text>
    </v-card>

    <!-- FK Constraints -->
    <v-card v-if="data.FkConstraints.length" class="mb-4">
      <v-card-title>{{ t('describe.fkConstraints') }} ({{ data.FkConstraints.length }})</v-card-title>
      <v-card-text>
        <v-data-table
          :headers="constraintHeaders"
          :items="data.FkConstraints"
        >
          <template #item.Definition="{ value }">
            <code>{{ value }}</code>
          </template>
        </v-data-table>
      </v-card-text>
    </v-card>

    <!-- Referenced By -->
    <v-card v-if="data.ReferencedBy.length" class="mb-4">
      <v-card-title>{{ t('describe.referencedBy') }} ({{ data.ReferencedBy.length }})</v-card-title>
      <v-card-text>
        <v-data-table
          :headers="referencedByHeaders"
          :items="data.ReferencedBy"
        >
          <template #item.Definition="{ value }">
            <code>{{ value }}</code>
          </template>
        </v-data-table>
      </v-card-text>
    </v-card>
  </template>
</template>
