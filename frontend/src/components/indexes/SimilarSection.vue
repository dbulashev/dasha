<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getIndexesSimilar1,
  getIndexesSimilar2,
  getIndexesSimilar3,
} from '@/api/gen/default/default'
import type { IndexSimilar1, IndexSimilar2, IndexSimilar3 } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const emit = defineEmits<{ error: [msg: string] }>()

// --- Similar 1 ---
const similar1Headers = computed(() => [
  { title: t('header.table'), key: 'Table' },
  { title: t('header.uniqueIndexName'), key: 'I1UniqueIndexName' },
  { title: t('header.indexName2'), key: 'I2IndexName' },
  { title: t('header.uniqueIndexDef'), key: 'I1UniqueIndexDefinition' },
  { title: t('header.indexDef2'), key: 'I2IndexDefinition' },
  { title: t('header.usedInConstraint1'), key: 'I1UsedInConstraint' },
  { title: t('header.usedInConstraint2'), key: 'I2UsedInConstraint' },
])
const similar1Items = ref<IndexSimilar1[]>([])
const similar1Loading = ref(false)

// --- Similar 2 ---
const similar2Headers = computed(() => [
  { title: t('header.table'), key: 'Table' },
  { title: t('header.fkName'), key: 'FkName' },
  { title: t('header.fkName2'), key: 'FkName2' },
])
const similar2Items = ref<IndexSimilar2[]>([])
const similar2Loading = ref(false)

// --- Similar 3 ---
const similar3Headers = computed(() => [
  { title: t('header.table'), key: 'Table' },
  { title: t('header.indexName'), key: 'I1IndexName' },
  { title: t('header.indexName2'), key: 'I2IndexName' },
  { title: t('header.simplifiedDef'), key: 'SimplifiedIndexDefinition' },
  { title: t('header.indexDef1'), key: 'I1IndexDefinition' },
  { title: t('header.indexDef2'), key: 'I2IndexDefinition' },
  { title: t('header.usedInConstraint1'), key: 'I1UsedInConstraint' },
  { title: t('header.usedInConstraint2'), key: 'I2UsedInConstraint' },
])
const similar3Items = ref<IndexSimilar3[]>([])
const similar3Loading = ref(false)

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  const params = { cluster_name: clusterName.value, instance: hostName.value, database: databaseName.value }

  similar1Loading.value = true
  similar2Loading.value = true
  similar3Loading.value = true

  const [r1, r2, r3] = await Promise.allSettled([
    getIndexesSimilar1(params),
    getIndexesSimilar2(params),
    getIndexesSimilar3(params),
  ])

  if (r1.status === 'fulfilled') {
    similar1Items.value = assertOk(r1.value) ?? []
  } else {
    emit('error', String(r1.reason))
    similar1Items.value = []
  }
  similar1Loading.value = false

  if (r2.status === 'fulfilled') {
    similar2Items.value = assertOk(r2.value) ?? []
  } else {
    emit('error', String(r2.reason))
    similar2Items.value = []
  }
  similar2Loading.value = false

  if (r3.status === 'fulfilled') {
    similar3Items.value = assertOk(r3.value) ?? []
  } else {
    emit('error', String(r3.reason))
    similar3Items.value = []
  }
  similar3Loading.value = false
}

watch([clusterName, hostName, databaseName], () => load(), { immediate: true })
</script>

<template>
  <!-- Similar Indexes (Algorithm 1) -->
  <v-card class="mb-4">
    <v-card-title>{{ t('indexes.similar1') }}</v-card-title>
    <v-card-subtitle class="text-wrap">{{ t('indexes.similar1Hint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="similar1Headers" :items="similar1Items" :loading="similar1Loading" density="compact" multi-sort disable-pagination hide-default-footer />
    </v-card-text>
  </v-card>

  <!-- Similar Indexes (Algorithm 2) -->
  <v-card class="mb-4">
    <v-card-title>{{ t('indexes.similar2') }}</v-card-title>
    <v-card-subtitle class="text-wrap">{{ t('indexes.similar2Hint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="similar2Headers" :items="similar2Items" :loading="similar2Loading" density="compact" multi-sort disable-pagination hide-default-footer />
    </v-card-text>
  </v-card>

  <!-- Similar Indexes (Algorithm 3) -->
  <v-card class="mb-4">
    <v-card-title>{{ t('indexes.similar3') }}</v-card-title>
    <v-card-subtitle class="text-wrap">{{ t('indexes.similar3Hint') }}</v-card-subtitle>
    <v-card-text>
      <v-data-table :headers="similar3Headers" :items="similar3Items" :loading="similar3Loading" density="compact" multi-sort disable-pagination hide-default-footer />
    </v-card-text>
  </v-card>
</template>
