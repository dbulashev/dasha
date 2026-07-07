<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LogEntry } from '@/api/models'
import { fmtDateTime } from '@/utils/format'
import { severityColor } from './types'

const props = defineProps<{
  items: LogEntry[]
  loading: boolean
  dedup: boolean
  partial: boolean
  scanned: number
  hasMore: boolean
  message: string
  searched: boolean
}>()

const emit = defineEmits<{
  loadMore: []
}>()

const { t } = useI18n()

// Log entries have no natural unique id; add a synthetic index so the data
// table can track row expansion correctly.
const rows = computed(() => props.items.map((it, i) => ({ ...it, __index: i })))

const headers = computed(() => {
  if (props.dedup) {
    return [
      { title: t('logs.col.count'), key: 'count', width: 90 },
      { title: t('logs.col.lastSeen'), key: 'last_seen', width: 190 },
      { title: t('logs.col.severity'), key: 'severity', width: 96 },
      { title: t('logs.col.text'), key: 'text' },
    ]
  }
  // Host, database and user live in the expanded row (fields), so the message
  // column gets the remaining width instead of being squeezed into a narrow cell.
  return [
    { title: t('logs.col.time'), key: 'timestamp', width: 180 },
    { title: t('logs.col.severity'), key: 'severity', width: 96 },
    { title: t('logs.col.text'), key: 'text' },
  ]
})

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// Main-row message is truncated; the full text stays in the expanded row.
const MAX_TEXT_LEN = 140

// highlightText truncates to MAX_TEXT_LEN (appending an ellipsis when longer),
// escapes the text, then wraps case-insensitive matches of the search term in
// <mark>. Safe to render via v-html because the input is already HTML-escaped.
function highlightText(text: string | undefined): string {
  const raw = text ?? ''
  const truncated = raw.length > MAX_TEXT_LEN
  const safe = escapeHtml(truncated ? raw.slice(0, MAX_TEXT_LEN) : raw)
  const needle = props.message.trim()
  const html = needle
    ? safe.replace(new RegExp(escapeRegExp(escapeHtml(needle)), 'gi'), (m) => `<mark>${m}</mark>`)
    : safe
  return truncated ? html + '…' : html
}

// Fields shown in the expanded row, excluding empties, sorted by key.
function fieldRows(item: LogEntry): Array<[string, string]> {
  const f = item.fields ?? {}
  return Object.entries(f)
    .filter(([, v]) => v !== '' && v != null)
    .sort((a, b) => a[0].localeCompare(b[0]))
}
</script>

<template>
  <v-card>
    <v-card-text>
      <v-alert
        v-if="props.partial"
        type="warning"
        variant="tonal"
        density="compact"
        class="mb-3"
      >
        {{ t('logs.partial', { scanned: props.scanned }) }}
      </v-alert>

      <v-alert
        v-if="props.searched && !props.loading && props.items.length === 0"
        type="info"
        variant="tonal"
        class="mb-0"
      >
        {{ t('logs.noResults') }}
      </v-alert>

      <v-data-table
        v-if="props.loading || props.items.length > 0"
        :headers="headers"
        :items="rows"
        :loading="props.loading"
        loading-text=""
        show-expand
        item-value="__index"
        :items-per-page="-1"
        hide-default-footer
      >
        <template #item.timestamp="{ item }">
          <span class="text-no-wrap">{{ fmtDateTime(item.timestamp) }}</span>
        </template>
        <template #item.last_seen="{ item }">
          <span class="text-no-wrap">{{ fmtDateTime(item.last_seen) }}</span>
        </template>
        <template #item.count="{ item }">
          <v-chip size="small" variant="tonal" color="primary">{{ item.count }}</v-chip>
        </template>
        <template #item.severity="{ item }">
          <v-chip size="x-small" label :color="severityColor(item.severity)" variant="tonal">
            {{ item.severity }}
          </v-chip>
        </template>
        <template #item.text="{ item }">
          <span class="log-text" v-html="highlightText(item.text)"></span>
        </template>
        <template #expanded-row="{ columns, item }">
          <tr>
            <td :colspan="columns.length" class="py-2 expanded-cell">
              <div class="d-flex flex-wrap ga-3">
                <span v-for="[k, v] in fieldRows(item)" :key="k" class="text-caption field-item">
                  <strong>{{ k }}:</strong> {{ v }}
                </span>
              </div>
            </td>
          </tr>
        </template>
      </v-data-table>

      <div v-if="props.hasMore" class="text-center mt-3">
        <v-btn
          variant="tonal"
          color="primary"
          :loading="props.loading"
          @click="emit('loadMore')"
        >
          {{ t('logs.loadMore') }}
        </v-btn>
      </div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.expanded-cell {
  padding-left: 2.5rem !important;
}

.log-text {
  white-space: pre-wrap;
  word-break: break-word;
  font-family: var(--v-font-monospace, monospace);
  font-size: 0.8125rem;
}

.field-item {
  max-width: 100%;
  word-break: break-word;
}
</style>
