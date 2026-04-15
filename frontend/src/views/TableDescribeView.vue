<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getTablesDescribe } from '@/api/gen/default/default'
import type { TableDescribe } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { useViewError } from '@/composables/useViewError'
import DescribeTableSelector from '@/components/tables/DescribeTableSelector.vue'
import DescribeHeaderSection from '@/components/tables/DescribeHeaderSection.vue'
import DescribeColumnsSection from '@/components/tables/DescribeColumnsSection.vue'
import DescribeIndexesSection from '@/components/tables/DescribeIndexesSection.vue'
import DescribeConstraintsSection from '@/components/tables/DescribeConstraintsSection.vue'
import DescribeReferencedBySection from '@/components/tables/DescribeReferencedBySection.vue'
import DescribePartitionsSection from '@/components/tables/DescribePartitionsSection.vue'
import DescribeBloatSection from '@/components/tables/DescribeBloatSection.vue'
import DescribeVacuumStatsSection from '@/components/tables/DescribeVacuumStatsSection.vue'
import DescribeRowEstimateSection from '@/components/tables/DescribeRowEstimateSection.vue'

const { t } = useI18n()
const route = useRoute()
const { clusterName, databaseName, hostName } = useClusterInfo()

const { onError: setError } = useViewError()
const data = ref<TableDescribe | null>(null)
const loading = ref(false)

const schema = computed(() => route.query.schema ? String(route.query.schema) : '')
const table = computed(() => route.query.table ? String(route.query.table) : '')

async function loadDescribe() {
  if (!clusterName.value || !hostName.value || !databaseName.value || !schema.value || !table.value) {
    data.value = null
    return
  }
  loading.value = true
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
    setError(getErrorMessage(err), err)
    data.value = null
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName, databaseName, schema, table], () => {
  loadDescribe()
}, { immediate: true })

const isPartitioned = computed(() => data.value?.TableType === 'partitioned_table')
</script>

<template>

  <DescribeTableSelector :loading="loading" />

  <template v-if="data">
    <DescribeHeaderSection :data="data" />
    <DescribeColumnsSection :items="data.Columns" />
    <DescribeIndexesSection :items="data.Indexes" />
    <DescribeConstraintsSection :title="t('describe.checkConstraints')" :items="data.CheckConstraints" />
    <DescribeConstraintsSection :title="t('describe.fkConstraints')" :items="data.FkConstraints" />
    <DescribeReferencedBySection :items="data.ReferencedBy" />
    <DescribePartitionsSection v-if="isPartitioned" :schema="schema" :table="table" />
    <DescribeBloatSection v-if="data.TableType === 'table'" :schema="schema" :table="table" />
    <DescribeVacuumStatsSection v-if="data.TableType === 'table' || data.TableType === 'materialized_view'" :schema="schema" :table="table" />
    <DescribeRowEstimateSection v-if="data.TableType === 'table' || data.TableType === 'materialized_view'" :schema="schema" :table="table" />
  </template>
</template>
