<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { QueryCompareItem } from '@/api/models/index'
import type { CompareSortKey } from './compare-types'
import { highlightSql, copyToClipboard } from '@/utils/sql'
import CompareMetricsBlock from './CompareMetricsBlock.vue'
import '@/assets/sql-highlight.css'

defineProps<{
  item: QueryCompareItem
  sortBy: CompareSortKey
}>()

const emit = defineEmits<{
  showSql: [item: QueryCompareItem]
}>()

const { t } = useI18n()

function truncateSql(sql: string, maxLen = 120): string {
  if (sql.length <= maxLen) return sql
  return sql.substring(0, maxLen) + '…'
}
</script>

<template>
  <v-card variant="outlined" class="mb-3">
    <!-- Common header: queryid + SQL -->
    <v-card-title class="text-body-1 pb-1 d-flex align-center">
      <span>queryid: <span class="text-mono">{{ item.QueryID }}</span></span>
      <v-btn icon="mdi-content-copy" variant="text" size="x-small" class="ml-1" @click="copyToClipboard(String(item.QueryID))" />
    </v-card-title>
    <v-card-text class="pt-0">
      <div class="mb-3 d-flex align-center">
        <code class="sql-highlight text-mono text-body-2 text-medium-emphasis flex-grow-1 sql-truncate" v-html="highlightSql(truncateSql(item.Query))"></code>
        <v-btn icon="mdi-content-copy" variant="text" size="x-small" class="ml-1 flex-shrink-0" @click="copyToClipboard(item.Query)" />
        <v-btn v-if="item.Query.length > 120" size="small" variant="text" class="ml-1 flex-shrink-0" @click="emit('showSql', item)">
          {{ t('report.showSql') }}
        </v-btn>
      </div>

      <!-- Two metric blocks A | B -->
      <v-row>
        <v-col v-for="side in [{ label: 'A', data: item.Left }, { label: 'B', data: item.Right }]" :key="side.label" cols="12" md="6">
          <div class="text-caption font-weight-bold mb-2">{{ side.label }}</div>
          <CompareMetricsBlock v-if="side.data" :metrics="side.data" :sort-by="sortBy" />
          <div v-else class="compare-absent text-medium-emphasis pa-4 text-center">
            {{ t('compare.absent') }}
          </div>
        </v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.sql-truncate {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.compare-absent {
  background-color: rgba(var(--v-theme-on-surface), 0.04);
  border: 1px dashed rgba(var(--v-theme-on-surface), 0.2);
  border-radius: 4px;
  min-height: 80px;
  display: flex;
  align-items: center;
  justify-content: center;
}
</style>
