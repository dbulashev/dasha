<script setup lang="ts">
import { highlightSql, copyToClipboard } from '@/utils/sql'
import '@/assets/sql-highlight.css'

const props = defineProps<{
  modelValue: boolean
  queryId: number
  sql: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()
</script>

<template>
  <v-dialog :model-value="props.modelValue" max-width="900" @update:model-value="emit('update:modelValue', $event)">
    <v-card>
      <v-card-title class="d-flex align-center">
        <span>queryid: {{ props.queryId }}</span>
        <v-spacer />
        <v-btn icon="mdi-content-copy" variant="text" size="small" @click="copyToClipboard(props.sql)" />
        <v-btn icon="mdi-close" variant="text" size="small" @click="emit('update:modelValue', false)" />
      </v-card-title>
      <v-card-text>
        <pre class="sql-highlight text-body-2" style="white-space: pre-wrap; word-break: break-word; font-family: monospace;" v-html="highlightSql(props.sql)"></pre>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

