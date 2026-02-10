<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TableDescribe } from '@/api/models/index'
import { fmtRowCount } from '@/utils/format'

const props = defineProps<{ data: TableDescribe }>()

const { t } = useI18n()

const title = computed(() => `${props.data.Schema}.${props.data.TableName}`)

const tableTypeLabel = computed(() => {
  switch (props.data.TableType) {
    case 'partitioned_table': return t('describe.partitionedTable')
    case 'table': return t('describe.table')
    default: return props.data.TableType
  }
})
</script>

<template>
  <!-- Metadata -->
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-2">
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
  <v-card v-if="data.SizeTotal || data.EstimatedRows" class="mb-4">
    <v-card-title>{{ t('describe.size') }}</v-card-title>
    <v-card-text>
      <v-row>
        <v-col v-if="data.EstimatedRows != null" cols="6" sm="2">
          <div class="text-caption text-medium-emphasis">{{ t('describe.estimatedRows') }}</div>
          <div class="text-h6">≈ {{ fmtRowCount(data.EstimatedRows) }}</div>
        </v-col>
        <v-col cols="6" sm="2">
          <div class="text-caption text-medium-emphasis">{{ t('describe.sizeTotal') }}</div>
          <div class="text-h6">{{ data.SizeTotal }}</div>
        </v-col>
        <v-col cols="6" sm="2">
          <div class="text-caption text-medium-emphasis">{{ t('describe.sizeTable') }}</div>
          <div class="text-h6">{{ data.SizeTable }}</div>
        </v-col>
        <v-col cols="6" sm="2">
          <div class="text-caption text-medium-emphasis">{{ t('describe.sizeToast') }}</div>
          <div class="text-h6">{{ data.SizeToast }}</div>
        </v-col>
        <v-col cols="6" sm="2">
          <div class="text-caption text-medium-emphasis">{{ t('describe.sizeIndexes') }}</div>
          <div class="text-h6">{{ data.SizeIndexes }}</div>
        </v-col>
      </v-row>
    </v-card-text>
    <v-card-text v-if="data.StatInfo" class="pt-0">
      <v-icon size="x-small" class="mr-1">mdi-chart-bar</v-icon>
      <span class="text-caption">{{ data.StatInfo }}</span>
    </v-card-text>
  </v-card>
</template>
