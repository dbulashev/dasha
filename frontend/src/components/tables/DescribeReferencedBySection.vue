<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TableDescribeReferencedBy } from '@/api/models/index'

defineProps<{ items: TableDescribeReferencedBy[] }>()

const { t } = useI18n()

const headers = computed(() => [
  { title: t('describe.constraintName'), key: 'Name' },
  { title: t('describe.sourceTable'), key: 'SourceTable' },
  { title: t('header.definition'), key: 'Definition' },
])
</script>

<template>
  <v-card v-if="items.length" class="mb-4">
    <v-card-title>{{ t('describe.referencedBy') }} ({{ items.length }})</v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items">
        <template #item.Definition="{ value }">
          <code>{{ value }}</code>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
