<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TableDescribeIndex } from '@/api/models/index'

defineProps<{ items: TableDescribeIndex[] }>()

const { t } = useI18n()

const headers = computed(() => [
  { title: t('describe.indexName'), key: 'Name' },
  { title: t('header.definition'), key: 'Definition' },
  { title: t('header.size'), key: 'Size' },
])
</script>

<template>
  <v-card v-if="items.length" class="mb-4">
    <v-card-title>{{ t('describe.indexes') }} ({{ items.length }})</v-card-title>
    <v-card-text>
      <v-data-table :headers="headers" :items="items">
        <template #item.Name="{ item }">
          <span class="d-inline-flex align-center">
            {{ item.Name }}
            <v-tooltip v-if="item.IsPrimary" :text="t('header.primary')" location="top">
              <template #activator="{ props }">
                <v-icon v-bind="props" icon="mdi-key" color="warning" size="small" class="ml-1" />
              </template>
            </v-tooltip>
            <v-tooltip v-if="item.IsUnique" :text="t('header.unique')" location="top">
              <template #activator="{ props }">
                <v-icon v-bind="props" icon="mdi-fingerprint" color="info" size="small" class="ml-1" />
              </template>
            </v-tooltip>
            <v-tooltip v-if="!item.IsValid" :text="t('describe.indexInvalid')" location="top">
              <template #activator="{ props }">
                <v-icon v-bind="props" icon="mdi-alert-circle" color="error" size="small" class="ml-1" />
              </template>
            </v-tooltip>
          </span>
        </template>
        <template #item.Definition="{ value }">
          <code>{{ value }}</code>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
