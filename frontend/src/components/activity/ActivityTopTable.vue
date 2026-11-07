<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import SqlDialog from '@/components/queries/SqlDialog.vue'
import { fmtDecimal } from '@/utils/format'
import { queryReportLink } from '@/utils/queryLink'
import { highlightSql, isUtilityStatement, truncateSql } from '@/utils/sql'
import '@/assets/sql-highlight.css'
import ActivityClassBar from './ActivityClassBar.vue'
import { BACKGROUND, DIMENSIONS, queryKeyLabel, type AggregateRow, type Dimension, type QueryInfo, type SessionInfo } from './activityBuffer'

const props = defineProps<{
  rows: AggregateRow[]
  cluster: string
  host: string
  trackUtility: boolean
  queryInfo: (key: string) => QueryInfo | undefined
  sessionInfo: (pid: number) => SessionInfo | undefined
  lockPids: Set<number>
  eventLabel: (value: string) => string
}>()

const dimension = defineModel<Dimension>('dimension', { required: true })

const emit = defineEmits<{
  filter: [dimension: Dimension, value: string]
}>()

const { t } = useI18n()

const HINTED = [
  { key: 'aas', hint: 'activity.aasHint' },
  { key: 'sharePct', hint: 'activity.shareHint' },
  { key: 'byClass', hint: 'activity.classesHint' },
]

const sortBy = [{ key: 'aas', order: 'desc' as const }]

const headers = computed(() => {
  const fixedTitles: Partial<Record<Dimension, string>> = { query: 'query_id', session: 'PID' }
  const title = fixedTitles[dimension.value] ?? t(`activity.dim.${dimension.value}`)
  const value = { title, key: 'value', sortable: false }
  const tail = [
    { title: t('activity.aas'), key: 'aas', align: 'end' as const },
    { title: t('activity.share'), key: 'sharePct', align: 'end' as const },
    // An event belongs to exactly one class.
    ...(dimension.value === 'event' ? [] : [{ title: t('activity.classes'), key: 'byClass', sortable: false }]),
  ]

  if (dimension.value === 'query') {
    return [
      value,
      { title: t('activity.text'), key: 'text', sortable: false, cellProps: { class: 'activity-text-cell' } },
      ...tail,
    ]
  }

  if (dimension.value === 'session') {
    return [
      value,
      { title: t('activity.user'), key: 'user', sortable: false },
      { title: t('activity.database'), key: 'database', sortable: false },
      { title: t('activity.application'), key: 'application', sortable: false },
      { title: t('activity.clientAddr'), key: 'clientAddr', sortable: false },
      { title: t('activity.state'), key: 'state', sortable: false },
      ...tail,
    ]
  }

  return [value, ...tail]
})

const items = computed(() =>
  props.rows.map((r) => {
    if (dimension.value === 'query') {
      const q = props.queryInfo(r.value)
      return { ...r, query: q, text: q?.text ?? '' }
    }
    if (dimension.value === 'session') {
      const s = props.sessionInfo(Number(r.value))
      return {
        ...r,
        user: s?.user ?? '',
        database: s?.database ?? '',
        application: s?.application ?? '',
        clientAddr: s?.clientAddr ?? '',
        state: s?.state ?? '',
      }
    }
    return r
  }),
)

function displayValue(value: string): string {
  if (dimension.value === 'event') return props.eventLabel(value)
  if (value === BACKGROUND) return t('activity.background')
  if (dimension.value === 'query') return value === '' ? t('activity.noText') : queryKeyLabel(value)
  return value === '' ? '—' : value
}

function reportLink(info: QueryInfo | undefined) {
  if (!info || info.kind !== 'query_id' || info.queryId === '0') return null
  if (!props.trackUtility && isUtilityStatement(info.text)) return null
  return queryReportLink(props.cluster, props.host, info.queryId)
}

function locksLink(pid: string, database: string) {
  if (!props.lockPids.has(Number(pid))) return null
  return {
    name: 'locks',
    params: { clustername: props.cluster },
    query: { host: props.host, ...(database ? { db: database } : {}) },
  }
}

const sqlVisible = ref(false)
const sqlText = ref('')
const sqlKey = ref('')

function showSql(key: string, text: string) {
  sqlKey.value = key
  sqlText.value = text
  sqlVisible.value = true
}
</script>

<template>
  <v-card>
    <v-tabs v-model="dimension" density="compact" show-arrows>
      <v-tab v-for="d in DIMENSIONS" :key="d" :value="d">{{ t(`activity.dim.${d}`) }}</v-tab>
    </v-tabs>

    <v-data-table
      :headers="headers"
      :items="items"
      :items-per-page="25"
      density="compact"
      :sort-by="sortBy"
      :no-data-text="t('activity.noRows')"
    >
      <template #item.value="{ item }">
        <div class="d-flex align-center ga-1">
          <div v-if="dimension === 'query' || dimension === 'session'" class="icon-slot">
            <v-btn
              v-if="'query' in item && reportLink(item.query)"
              :to="reportLink(item.query)!"
              icon="mdi-file-chart-outline"
              size="x-small"
              variant="text"
              :title="t('activity.openReport')"
            />
            <v-btn
              v-else-if="dimension === 'session' && 'database' in item && locksLink(item.value, String(item.database))"
              :to="locksLink(item.value, String(item.database))!"
              icon="mdi-lock-outline"
              size="x-small"
              variant="text"
              :title="t('activity.openLocks')"
            />
          </div>
          <span v-if="item.value === BACKGROUND || (dimension === 'query' && item.value === '')" class="text-medium-emphasis">
            {{ displayValue(item.value) }}
          </span>
          <a
            v-else
            href="#"
            :class="{ mono: dimension === 'query' || dimension === 'session' }"
            :title="t('activity.addFilter')"
            @click.prevent="emit('filter', dimension, item.value)"
          >
            {{ displayValue(item.value) }}
          </a>
          <v-chip
            v-if="dimension === 'query' && 'query' in item && item.query && item.query.kind !== 'query_id' && item.query.kind !== 'none'"
            size="x-small"
            variant="tonal"
            color="warning"
            :title="t(`activity.keyKindHint.${item.query.kind}`)"
          >
            ≈
          </v-chip>
        </div>
      </template>

      <template #item.text="{ item }">
        <button
          v-if="'text' in item && item.text"
          type="button"
          class="sql-preview"
          :title="t('activity.showText')"
          @click="showSql(queryKeyLabel(item.value), String(item.text))"
        >
          <code class="sql-highlight text-body-2 text-truncate d-block" v-html="highlightSql(truncateSql(String(item.text)))" />
        </button>
      </template>

      <template v-for="h in HINTED" :key="h.key" #[`header.${h.key}`]="{ column }">
        <v-tooltip :text="t(h.hint)" location="top" max-width="380">
          <template #activator="{ props: tip }">
            <span v-bind="tip" class="hinted">{{ column.title }}</span>
          </template>
        </v-tooltip>
      </template>

      <template #item.aas="{ item }">{{ fmtDecimal(item.aas, 2) }}</template>
      <template #item.sharePct="{ item }">{{ fmtDecimal(item.sharePct, 1) }}%</template>
      <template #item.byClass="{ item }">
        <ActivityClassBar :by-class="item.byClass" />
      </template>
    </v-data-table>

    <SqlDialog v-model="sqlVisible" :query-id="sqlKey" :sql="sqlText" :label="t('activity.dim.query')" />
  </v-card>
</template>

<style scoped>
.mono {
  font-family: 'Roboto Mono', ui-monospace, monospace;
}

.icon-slot {
  display: flex;
  width: 28px;
  flex-shrink: 0;
}

.sql-preview {
  display: block;
  width: 100%;
  min-width: 0;
  padding: 0;
  text-align: left;
  cursor: pointer;
  background: none;
  border: 0;
  color: inherit;
}

.sql-preview:hover code {
  text-decoration: underline dotted;
  text-underline-offset: 3px;
}

:deep(.activity-text-cell) {
  width: 100%;
  max-width: 0;
}

.hinted {
  white-space: nowrap;
  cursor: help;
  text-decoration: underline dotted;
  text-underline-offset: 3px;
}
</style>
