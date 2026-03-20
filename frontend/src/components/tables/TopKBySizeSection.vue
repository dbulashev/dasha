<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getTablesTopKBySize } from '@/api/gen/default/default'
import type { TableTopKBySize } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.table'), key: 'Table' },
  { title: t('header.nIdx'), key: 'NIdx' },
  { title: t('header.total'), key: 'TotalBytes' },
  { title: t('header.toast'), key: 'Toast' },
  { title: t('header.indexes'), key: 'Indexes' },
  { title: t('header.main'), key: 'Main' },
  { title: t('header.fsm'), key: 'Fsm' },
  { title: t('header.vm'), key: 'Vm' },
  { title: t('header.bloat'), key: 'Bloat' },
])
const limitOptions = [10, 20, 30, 50]
const limit = ref(10)

const { items, loading } = useApiLoader<TableTopKBySize[]>(
  () => getTablesTopKBySize({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
    limit: limit.value,
  }),
  {
    deps: [clusterName, hostName, databaseName, limit],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError: (msg) => emit('error', msg),
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      {{ t('tables.topKBySize') }}
      <v-tooltip :text="t('hint.tableBloat')" location="bottom">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
      <v-select
        v-model="limit"
        :items="limitOptions"
        density="compact"
        hide-details
        class="ml-4"
        style="max-width: 100px"
      />
    </v-card-title>
    <v-card-text>
      <div class="text-caption text-medium-emphasis mb-3">
        <div><strong>MAIN / TOAST / FSM / VM</strong> — {{ t('tables.statHintLayers') }}</div>
        <div><strong>INS / UPD / DEL</strong> — {{ t('tables.statHintDml') }}</div>
        <div><strong>HOT UPD</strong> — {{ t('tables.statHintHotUpd') }}</div>
        <div><strong>SEQ_SCN / IDX_SCN</strong> — {{ t('tables.statHintSeqScn') }}</div>
      </div>
      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
        :expanded="items.map(i => i.Table)"
        item-value="Table"
      >
        <template #item.TotalBytes="{ item }">{{ item.Total }}</template>
        <template #expanded-row="{ columns, item }">
          <tr v-if="item.StatInfo || item.Options" class="topk-expanded-row">
            <td :colspan="columns.length" class="py-1 expanded-cell">
              <v-icon size="x-small" class="mr-1 text-medium-emphasis">mdi-subdirectory-arrow-right</v-icon>
              <span v-if="item.StatInfo" class="text-caption mr-4">
                <v-icon size="x-small" class="mr-1">mdi-chart-bar</v-icon>{{ item.StatInfo }}
              </span>
              <span v-if="item.Options" class="text-caption">
                <v-icon size="x-small" class="mr-1">mdi-cog-outline</v-icon>{{ item.Options }}
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
