<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIndexesSimilar3 } from '@/api/gen/default/default'
import type { IndexSimilar3 } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useDescribeLink } from '@/composables/useDescribeLink'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { describeLinkFromQualified } = useDescribeLink()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

const headers = computed(() => [
  { title: t('header.table'), key: 'Table' },
  { title: t('header.indexName'), key: 'I1IndexName' },
  { title: t('header.indexName2'), key: 'I2IndexName' },
  { title: t('header.simplifiedDef'), key: 'SimplifiedIndexDefinition' },
  { title: t('header.indexDef1'), key: 'I1IndexDefinition' },
  { title: t('header.indexDef2'), key: 'I2IndexDefinition' },
  { title: t('header.usedInConstraint1'), key: 'I1UsedInConstraint' },
  { title: t('header.usedInConstraint2'), key: 'I2UsedInConstraint' },
])

const { items, loading } = useApiLoader<IndexSimilar3[]>(
  () => getIndexesSimilar3({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
    database: databaseName.value!,
  }),
  {
    deps: [clusterName, hostName, databaseName],
    guard: () => !!clusterName.value && !!hostName.value && !!databaseName.value,
    onError: (msg) => emit('error', msg),
  },
)
</script>

<template>
  <v-card class="mb-4">
    <v-card-title><v-icon start icon="mdi-content-duplicate" />{{ t('indexes.similar3') }}</v-card-title>
    <v-card-subtitle class="text-wrap">{{ t('indexes.similar3Hint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="headers" :items="items" :loading="loading">
        <template #item.Table="{ item }">
          <router-link :to="describeLinkFromQualified(item.Table)" class="text-decoration-none">{{ item.Table }}</router-link>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
