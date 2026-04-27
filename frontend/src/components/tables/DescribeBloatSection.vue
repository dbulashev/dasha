<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getPgstattupleAvailable, getTablesDescribeBloat } from '@/api/gen/default/default'
import type { TableDescribeBloat } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtRowCount } from '@/utils/format'

const props = defineProps<{ schema: string; table: string }>()

const { t } = useI18n()
const { clusterName, hostName, databaseName } = useClusterInfo()

const pgstattupleAvailable = ref(false)
const bloatData = ref<TableDescribeBloat | null>(null)
const bloatLoading = ref(false)
const bloatError = ref('')

watch([clusterName, hostName, databaseName], async () => {
  pgstattupleAvailable.value = false
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  try {
    const res = await getPgstattupleAvailable({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
    })
    const body = assertOk(res) as { Available: boolean }
    pgstattupleAvailable.value = body.Available
  } catch {
    pgstattupleAvailable.value = false
  }
}, { immediate: true })

watch([clusterName, hostName, databaseName, () => props.schema, () => props.table], () => {
  bloatData.value = null
  bloatError.value = ''
})

async function calculateBloat() {
  if (!clusterName.value || !hostName.value || !databaseName.value || !props.schema || !props.table) return
  bloatLoading.value = true
  bloatError.value = ''
  try {
    const res = await getTablesDescribeBloat({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      schema: props.schema,
      table: props.table,
    })
    bloatData.value = assertOk(res) as TableDescribeBloat
  } catch (err) {
    bloatError.value = getErrorMessage(err)
    bloatData.value = null
  } finally {
    bloatLoading.value = false
  }
}

function formatTupleCount(n: number): string {
  return fmtRowCount(n) + ' ' + t('describe.bloatRows', n >= 1_000 ? 5 : n)
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center">
      <span>Bloat</span>
      <v-chip v-if="!pgstattupleAvailable" size="x-small" color="warning" variant="tonal" class="ml-2">
        pgstattuple {{ t('describe.bloatExtNotInstalled') }}
      </v-chip>
    </v-card-title>
    <v-card-text v-if="bloatData">
      <v-row>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">{{ t('describe.bloatDeadTuples') }}</div>
          <div class="text-h6">{{ bloatData.DeadTupleLenPretty }}</div>
          <div class="text-caption text-medium-emphasis">{{ bloatData.DeadTuplePercent.toFixed(1) }}% · {{ formatTupleCount(bloatData.DeadTupleCount) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">{{ t('describe.bloatFreeSpace') }}</div>
          <div class="text-h6">{{ bloatData.ApproxFreeSpacePretty }}</div>
          <div class="text-caption text-medium-emphasis">{{ bloatData.ApproxFreePercent.toFixed(1) }}%</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">{{ t('describe.bloatLiveData') }}</div>
          <div class="text-h6">{{ bloatData.ApproxTupleLenPretty }}</div>
          <div class="text-caption text-medium-emphasis">{{ bloatData.ApproxTuplePercent.toFixed(1) }}% · {{ formatTupleCount(bloatData.ApproxTupleCount) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">{{ t('describe.bloatTableLen') }}</div>
          <div class="text-h6">{{ bloatData.TableLenPretty }}</div>
        </v-col>
      </v-row>
    </v-card-text>
    <v-card-text v-if="bloatError">
      <v-alert type="error" density="compact">{{ bloatError }}</v-alert>
    </v-card-text>
    <v-card-actions v-if="pgstattupleAvailable && !bloatData">
      <v-btn
        variant="tonal"
        prepend-icon="mdi-magnify-scan"
        :loading="bloatLoading"
        @click="calculateBloat"
      >
        {{ t('describe.bloatCalc') }}
      </v-btn>
    </v-card-actions>
  </v-card>
</template>
