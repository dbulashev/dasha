<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LogEntry } from '@/api/models'
import { fmtDateTime } from '@/utils/format'
import { highlightSql, copyToClipboard } from '@/utils/sql'
import '@/assets/sql-highlight.css'
import { severityColor } from './types'

const props = defineProps<{
  items: LogEntry[]
  loading: boolean
  dedup: boolean
  partial: boolean
  scanned: number
  hasMore: boolean
  // Applied include terms, highlighted in the message column.
  messages: string[]
  searched: boolean
}>()

const emit = defineEmits<{
  loadMore: []
  filter: [field: 'severity' | 'user' | 'database' | 'host', value: string]
  exclude: [text: string]
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
// escapes the text, then wraps case-insensitive matches of the search terms in
// <mark>. A single alternation pass keeps one term from matching inside the
// markup another term inserted. Safe to render via v-html because the input is
// already HTML-escaped.
function highlightText(text: string | undefined): string {
  const raw = text ?? ''
  const truncated = raw.length > MAX_TEXT_LEN
  const safe = escapeHtml(truncated ? raw.slice(0, MAX_TEXT_LEN) : raw)
  const needles = props.messages.map(m => m.trim()).filter(Boolean)
  const html = needles.length
    ? safe.replace(
        new RegExp(needles.map(n => escapeRegExp(escapeHtml(n))).join('|'), 'gi'),
        (m) => `<mark>${m}</mark>`,
      )
    : safe
  return truncated ? html + '…' : html
}

// Field-map keys that drill into a search filter on click.
const DRILL_KEYS: Record<string, 'user' | 'database' | 'host'> = {
  user_name: 'user',
  user: 'user',
  database_name: 'database',
  db: 'database',
  hostname: 'host',
}

// SQL-bearing keys get their own highlighted blocks instead of inline pairs.
const SQL_FIELD_KEYS = ['query', 'internal_query']

// Fields shown in the expanded row, excluding empties and zero-valued ids
// (query_id=0 and friends carry no information), sorted by key.
function fieldRows(item: LogEntry): Array<[string, string]> {
  const f = item.fields ?? {}
  return Object.entries(f)
    .filter(([k, v]) => v !== '' && v != null && v !== '0' && !SQL_FIELD_KEYS.includes(k))
    .sort((a, b) => a[0].localeCompare(b[0]))
}

function sqlRows(item: LogEntry): Array<[string, string]> {
  const f = item.fields ?? {}
  return SQL_FIELD_KEYS.filter(k => f[k]).map(k => [k, f[k]] as [string, string])
}

function copyEntry(item: LogEntry, what: 'all' | 'query' | 'message') {
  const f = item.fields ?? {}
  let text = ''
  if (what === 'query') {
    text = f.query ?? ''
  } else if (what === 'message') {
    text = item.text || f.message || ''
  } else {
    text = Object.entries(f)
      .filter(([, v]) => v !== '' && v != null)
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([k, v]) => `${k}: ${v}`)
      .join('\n')
  }
  copyToClipboard(text)
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
          <v-chip
            size="x-small"
            label
            :color="severityColor(item.severity)"
            variant="tonal"
            class="drill"
            :title="t('logs.drill')"
            @click="item.severity && emit('filter', 'severity', item.severity)"
          >
            {{ item.severity }}
          </v-chip>
        </template>
        <template #item.text="{ item }">
          <span class="log-text" v-html="highlightText(item.text)"></span>
        </template>
        <template #expanded-row="{ columns, item }">
          <tr>
            <td :colspan="columns.length" class="py-2 expanded-cell">
              <div class="d-flex align-start">
                <div class="d-flex flex-wrap ga-3 flex-grow-1">
                  <span v-for="[k, v] in fieldRows(item)" :key="k" class="text-caption field-item">
                    <strong>{{ k }}:</strong>
                    <a
                      v-if="DRILL_KEYS[k]"
                      class="drill-value"
                      :title="t('logs.drill')"
                      @click.prevent="emit('filter', DRILL_KEYS[k], v)"
                    >{{ v }}</a>
                    <template v-else>{{ v }}</template>
                  </span>
                </div>
                <v-menu>
                  <template #activator="{ props: menuProps }">
                    <v-btn
                      v-bind="menuProps"
                      icon="mdi-content-copy"
                      size="x-small"
                      variant="text"
                      :title="t('logs.copy.title')"
                    />
                  </template>
                  <v-list density="compact">
                    <v-list-item @click="copyEntry(item, 'all')">
                      <v-list-item-title>{{ t('logs.copy.all') }}</v-list-item-title>
                    </v-list-item>
                    <v-list-item :disabled="!item.fields?.query" @click="copyEntry(item, 'query')">
                      <v-list-item-title>{{ t('logs.copy.query') }}</v-list-item-title>
                    </v-list-item>
                    <v-list-item @click="copyEntry(item, 'message')">
                      <v-list-item-title>{{ t('logs.copy.message') }}</v-list-item-title>
                    </v-list-item>
                    <v-divider />
                    <!-- For dedup rows item.text is the masked template ("<*>"),
                         which the backend matches against each record's template —
                         so the whole group shape gets excluded, not one member. -->
                    <v-list-item
                      :disabled="!item.text"
                      @click="emit('exclude', item.text || '')"
                    >
                      <v-list-item-title>{{ t('logs.copy.exclude') }}</v-list-item-title>
                    </v-list-item>
                  </v-list>
                </v-menu>
              </div>
              <div v-for="[k, v] in sqlRows(item)" :key="k" class="mt-2">
                <div class="text-caption text-medium-emphasis">{{ k }}</div>
                <!-- Safe v-html: highlightSql output is hljs-generated markup over escaped input. -->
                <pre class="log-sql"><code class="sql-highlight" v-html="highlightSql(v)"></code></pre>
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

.drill {
  cursor: pointer;
}

.drill-value {
  cursor: pointer;
  text-decoration: underline dotted;
  color: inherit;
}

.log-sql {
  background: rgba(128, 128, 128, 0.08);
  padding: 6px 10px;
  border-radius: 4px;
  font-size: 0.8125rem;
  white-space: pre-wrap;
  word-break: break-word;
  overflow-x: auto;
  margin: 0;
}
</style>
