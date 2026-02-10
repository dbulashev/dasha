<script setup lang="ts">
import hljs from 'highlight.js/lib/core'
import pgsql from 'highlight.js/lib/languages/pgsql'

hljs.registerLanguage('pgsql', pgsql)

const props = defineProps<{
  modelValue: boolean
  queryId: number
  sql: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

function highlightSql(sql: string): string {
  return hljs.highlight(sql, { language: 'pgsql' }).value
}

function copyToClipboard(text: string) {
  if (navigator.clipboard) {
    navigator.clipboard.writeText(text)
  } else {
    const ta = document.createElement('textarea')
    ta.value = text
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
}
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

<style scoped>
.sql-highlight :deep(.hljs-keyword) { color: #cf222e; }
.sql-highlight :deep(.hljs-string) { color: #0a3069; }
.sql-highlight :deep(.hljs-number) { color: #0550ae; }
.sql-highlight :deep(.hljs-built_in) { color: #8250df; }
.sql-highlight :deep(.hljs-type) { color: #8250df; }
.sql-highlight :deep(.hljs-comment) { color: #6e7781; }
.sql-highlight :deep(.hljs-operator) { color: #cf222e; }

.v-theme--dark .sql-highlight :deep(.hljs-keyword) { color: #ff7b72; }
.v-theme--dark .sql-highlight :deep(.hljs-string) { color: #a5d6ff; }
.v-theme--dark .sql-highlight :deep(.hljs-number) { color: #79c0ff; }
.v-theme--dark .sql-highlight :deep(.hljs-built_in) { color: #d2a8ff; }
.v-theme--dark .sql-highlight :deep(.hljs-type) { color: #d2a8ff; }
.v-theme--dark .sql-highlight :deep(.hljs-comment) { color: #8b949e; }
.v-theme--dark .sql-highlight :deep(.hljs-operator) { color: #ff7b72; }
</style>
