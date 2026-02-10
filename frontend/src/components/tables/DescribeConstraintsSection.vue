<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TableDescribeConstraint } from '@/api/models/index'

const props = defineProps<{ title: string; items: TableDescribeConstraint[] }>()

const { t } = useI18n()

const headers = computed(() => [
  { title: t('describe.constraintName'), key: 'Name' },
  { title: t('header.definition'), key: 'Definition' },
])
</script>

<template>
  <v-card v-if="props.items.length" class="mb-4">
    <v-card-title>{{ props.title }} ({{ props.items.length }})</v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="props.items">
        <template #item.Definition="{ value }">
          <code>{{ value }}</code>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
