<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TableDescribeColumn } from '@/api/models/index'

defineProps<{ items: TableDescribeColumn[] }>()

const { t } = useI18n()

const headers = computed(() => [
  { title: t('describe.columnName'), key: 'Name' },
  { title: t('describe.type'), key: 'Type' },
  { title: t('describe.nullable'), key: 'Nullable' },
  { title: t('describe.default'), key: 'Default' },
  { title: t('describe.description'), key: 'Description' },
])

function hasExtra(item: TableDescribeColumn) {
  return !!item.Collation || (item.NullFrac != null && item.NullFrac > 0) || item.NDistinct != null || item.AvgWidth != null || !!item.Storage
}

function formatNullFrac(value: number | null | undefined): string {
  if (value == null) return '—'
  return (value * 100).toFixed(1) + '%'
}

function formatNDistinct(value: number | null | undefined): string {
  if (value == null) return '—'
  if (value >= 0) return Math.round(value).toLocaleString()
  if (value === -1) return t('describe.ndistinctAllUnique')
  return '×' + Math.abs(value).toFixed(2) + ' ' + t('describe.ndistinctOfRows')
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('describe.columns') }} ({{ items.length }})</v-card-title>
    <v-card-text>
      <v-data-table
        :headers="headers"
        :items="items"
        item-value="Name"
        show-expand
      >
        <template #item.Nullable="{ value }">
          <v-icon v-if="value" icon="mdi-check" color="success" size="small" />
        </template>
        <template #item.Default="{ value }">
          <code v-if="value">{{ value }}</code>
        </template>
        <template #expanded-row="{ columns, item }">
          <tr v-if="hasExtra(item)">
            <td :colspan="columns.length" class="py-1 expanded-cell">
              <v-icon size="x-small" class="mr-1 text-medium-emphasis">mdi-subdirectory-arrow-right</v-icon>
              <span v-if="item.Collation" class="text-caption mr-4">
                {{ t('describe.collation') }}: <strong>{{ item.Collation }}</strong>
              </span>
              <span v-if="item.Storage" class="text-caption mr-4">
                {{ t('describe.statStorage') }}: <strong>{{ item.Storage }}</strong>
              </span>
              <span v-if="item.NullFrac != null && item.NullFrac > 0" class="text-caption mr-4">
                {{ t('describe.statNullFrac') }}: <strong>{{ formatNullFrac(item.NullFrac) }}</strong>
              </span>
              <span v-if="item.NDistinct != null" class="text-caption mr-4">
                {{ t('describe.statNDistinct') }}: <strong>{{ formatNDistinct(item.NDistinct) }}</strong>
              </span>
              <span v-if="item.AvgWidth != null" class="text-caption">
                {{ t('describe.statAvgWidth') }}: <strong>{{ item.AvgWidth }} {{ t('describe.bytes') }}</strong>
              </span>
            </td>
          </tr>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.expanded-cell {
  padding-left: 2.5rem !important;
}
</style>
