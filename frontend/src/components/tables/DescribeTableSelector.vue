<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getTablesSchemas, getTablesSearch } from '@/api/gen/default/default'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useDebouncedRef } from '@/composables/useDebouncedRef'

defineProps<{ loading?: boolean }>()

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { clusterName, hostName, databaseName } = useClusterInfo()

// --- Schema ---
const selectedSchema = ref(route.query.schema ? String(route.query.schema) : 'public')

const { items: schemas, loading: schemasLoading } = useApiLoader<string[]>(
  () => getTablesSchemas({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
  }),
  {
    deps: [clusterName, hostName, databaseName],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError: () => {},
  },
)

watch(schemas, (val) => {
  if (val.length && !val.includes(selectedSchema.value)) {
    selectedSchema.value = val[0] ?? 'public'
  }
})

// --- Table search ---
const tableSearchInput = ref('')
const debouncedSearch = useDebouncedRef(tableSearchInput, 300)
const selectedTable = ref(route.query.table ? String(route.query.table) : '')

const { items: tableOptions, loading: searchLoading } = useApiLoader<string[]>(
  () => getTablesSearch({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
    schema: selectedSchema.value,
    q: debouncedSearch.value,
    limit: 50,
  }),
  {
    deps: [clusterName, hostName, databaseName, selectedSchema, debouncedSearch],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value && !!selectedSchema.value,
    onError: () => {},
  },
)

// Sync selection → URL
watch([selectedSchema, selectedTable], () => {
  if (!selectedSchema.value) return
  const { host, db } = route.query
  router.replace({
    name: route.name!,
    params: route.params,
    query: {
      ...(host ? { host: String(host) } : {}),
      ...(db ? { db: String(db) } : {}),
      schema: selectedSchema.value,
      ...(selectedTable.value ? { table: selectedTable.value } : {}),
    },
  })
})

// Sync URL → state
watch(() => [route.query.schema, route.query.table], () => {
  const urlSchema = route.query.schema ? String(route.query.schema) : ''
  const urlTable = route.query.table ? String(route.query.table) : ''
  if (urlSchema && urlSchema !== selectedSchema.value) selectedSchema.value = urlSchema
  if (urlTable && urlTable !== selectedTable.value) selectedTable.value = urlTable
})

// Reset table when schema changes
watch(selectedSchema, (newSchema, oldSchema) => {
  if (oldSchema && newSchema !== oldSchema) {
    selectedTable.value = ''
    tableSearchInput.value = ''
  }
})
</script>

<template>
  <v-card class="mb-4">
    <v-card-text>
      <v-row>
        <v-col cols="12" sm="4">
          <v-autocomplete
            v-model="selectedSchema"
            :items="schemas"
            :loading="schemasLoading"
            :label="t('describe.schemaLabel')"
            density="compact"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="8">
          <v-autocomplete
            v-model="selectedTable"
            v-model:search="tableSearchInput"
            :items="tableOptions"
            :loading="searchLoading"
            :label="t('describe.tableLabel')"
            :placeholder="t('Start typing...')"
            density="compact"
            hide-details
            :no-filter="true"
            clearable
          />
        </v-col>
      </v-row>
    </v-card-text>
    <v-progress-linear v-if="loading || schemasLoading" indeterminate color="primary" />
  </v-card>
</template>
