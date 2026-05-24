<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getTablesDescribeRowEstimate } from '@/api/gen/default/default'
import type { RowEstimate } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

const props = defineProps<{ schema: string; table: string }>()

const { t } = useI18n()
const { clusterName, hostName, databaseName } = useClusterInfo()

const data = ref<RowEstimate | null>(null)
const loading = ref(false)
const noStats = ref(false)

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value || !props.schema || !props.table) {
    data.value = null
    return
  }
  loading.value = true
  noStats.value = false
  try {
    const res = await getTablesDescribeRowEstimate({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      schema: props.schema,
      table: props.table,
    })
    const result = assertOk<RowEstimate>(res)
    if (result.ColumnsWithStats === 0) {
      noStats.value = true
      data.value = null
    } else {
      data.value = result
    }
  } catch (err) {
    console.error(getErrorMessage(err))
    data.value = null
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName, databaseName, () => props.schema, () => props.table], () => load(), { immediate: true })

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('describe.rowEstimate') }}</v-card-title>
    <v-card-text v-if="loading">
      <v-progress-linear indeterminate />
    </v-card-text>
    <v-card-text v-else-if="noStats">
      <v-alert type="info" density="compact">{{ t('describe.noStats') }}</v-alert>
    </v-card-text>
    <v-card-text v-else-if="data">
      <v-alert
        v-if="data.ColumnsWithStats < data.ColumnsTotal"
        type="warning"
        density="compact"
        class="mb-4"
      >
        {{ t('describe.statsIncomplete', { count: data.ColumnsWithStats, total: data.ColumnsTotal }) }}
        {{ t('describe.runAnalyze') }}
      </v-alert>

      <v-row class="mb-4">
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.tupleHeader') }}
            <v-tooltip :text="t('describe.hint.tupleHeader')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ data.TupleHeaderSize }} B</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.nullBitmap') }}
            <v-tooltip :text="t('describe.hint.nullBitmap')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ data.NullBitmapSize }} B</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.rowData') }}
            <v-tooltip :text="t('describe.hint.rowData')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ fmtBytes(data.SumAvgWidth) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.estimatedRowSize') }}
            <v-tooltip :text="t('describe.hint.estimatedRowSize')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ fmtBytes(data.EstimatedRowSize) }}</div>
        </v-col>
      </v-row>

      <v-alert
        v-if="data.WillToast"
        type="warning"
        density="compact"
        variant="tonal"
        class="mb-4"
      >
        {{ t('describe.willToast') }}
        ({{ fmtBytes(data.EstimatedRowSize) }} &gt; {{ fmtBytes(data.ToastThreshold) }})
      </v-alert>

      <v-row class="mb-4">
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.fillfactor') }}
            <v-tooltip :text="t('describe.hint.fillfactor')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ data.Fillfactor }}%</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.pageUsable') }}
            <v-tooltip :text="t('describe.hint.pageUsable')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ fmtBytes(data.PageUsable) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.availableSpace') }}
            <v-tooltip :text="t('describe.hint.availableSpace')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ fmtBytes(data.AvailableSpace) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.rowsPerPage') }}
            <v-tooltip :text="t('describe.hint.rowsPerPage')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ data.RowsPerPage }}</div>
        </v-col>
      </v-row>

      <v-row v-if="data.Fillfactor < 100" class="mb-4">
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.reservedForUpdates') }}
            <v-tooltip :text="t('describe.hint.reservedForUpdates')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ fmtBytes(data.ReservedSpace) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.reservedForHotUpdates') }}
            <v-tooltip :text="t('describe.hint.reservedForHotUpdates')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ data.RowsFitInReserved }} {{ t('describe.rowsFitInReserved') }}</div>
        </v-col>
      </v-row>

      <div v-if="data.ToastCandidates && data.ToastCandidates.length > 0">
        <div class="text-subtitle-2 mb-2">
          {{ t('describe.toastCandidates') }}
          <v-tooltip :text="t('describe.hint.toastCandidates')" location="top" max-width="400">
            <template #activator="{ props: tp }">
              <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
            </template>
          </v-tooltip>
        </div>
        <v-table density="compact">
          <thead>
            <tr>
              <th>{{ t('describe.columnName') }}</th>
              <th class="text-right">{{ t('describe.avgWidth') }}</th>
              <th>{{ t('describe.storage') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="col in data.ToastCandidates" :key="col.ColumnName">
              <td>{{ col.ColumnName }}</td>
              <td class="text-right">{{ fmtBytes(col.AvgWidth) }}</td>
              <td>
                <v-chip size="x-small" :color="col.Storage === 'plain' ? 'error' : 'default'" variant="tonal">
                  {{ col.Storage }}
                </v-chip>
              </td>
            </tr>
          </tbody>
        </v-table>
      </div>
    </v-card-text>
  </v-card>
</template>
