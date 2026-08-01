<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import type { SchemaLintFinding } from '@/api/models/index'
import { useDescribeLink } from '@/composables/useDescribeLink'
import { copyToClipboard } from '@/utils/sql'
import { LEVEL_COLOR, LEVEL_ICON } from './levels'

const props = defineProps<{
  findings: SchemaLintFinding[]
  loading: boolean
  groupBy: 'code' | 'object'
}>()

const { t, te } = useI18n()
const route = useRoute()
const { describeLink } = useDescribeLink()

const copied = ref<string | null>(null)

interface Row extends SchemaLintFinding {
  qualified: string
}

const rows = computed<Row[]>(() =>
  props.findings.map((f) => ({ ...f, qualified: `${f.schema}.${f.object}` })),
)

const headers = computed(() => [
  { title: t('schemaLint.table.level'), key: 'level', width: 130 },
  { title: t('schemaLint.table.check'), key: 'code' },
  { title: t('header.schema'), key: 'schema' },
  { title: t('schemaLint.table.object'), key: 'qualified' },
])

const groupByOption = computed(() => [
  { key: props.groupBy === 'code' ? 'code' : 'qualified', order: 'asc' as const },
])

// A finding is addressed by check plus object plus column: the same table can
// fail several checks, and a column-level check can fire on several of its
// columns — a key without the column would expand both rows at once.
function rowKey(item: SchemaLintFinding) {
  const p = item.params ?? {}
  const within = p.column ?? p.constraint ?? p.index ?? ''
  return `${item.code}:${item.schema}.${item.object}:${within}`
}

function checkTitle(code: string) {
  const key = `schemaLint.checks.${code}.title`
  return te(key) ? t(key) : code
}

// Every text gets every parameter — vue-i18n drops the ones a phrasing omits,
// and which fields the backend fills is the check's business, not the renderer's.
function textParams(f: SchemaLintFinding) {
  const p = f.params ?? {}
  return {
    schema: f.schema,
    object: f.object,
    usedPct: (p.used_pct ?? 0).toFixed(1),
    freePct: (100 - (p.used_pct ?? 0)).toFixed(1),
    lastValue: p.last_value ?? 0,
    maxValue: p.max_value ?? 0,
    ownedBy: p.owned_by ?? '',
    ownedColumnType: p.owned_column_type ?? '',
    owner: p.owner ?? '',
    column: p.column ?? '',
    columnType: p.column_type ?? '',
    constraint: p.constraint ?? '',
    referencedBy: p.referenced_by ?? '',
    index: p.index ?? '',
    otherObject: p.other_object ?? '',
    partitions: p.partitions ?? 0,
  }
}

function checkText(f: SchemaLintFinding, part: 'description' | 'howToFix') {
  const key = `schemaLint.checks.${f.code}.${part}`
  return te(key) ? t(key, textParams(f)) : ''
}

// Always quoted, never conditionally: the catalog gives the literal name, and
// quoting it reproduces exactly that name whatever it contains. This very report
// flags names that are keywords or need quoting, so the statement it offers for
// those objects has to parse. Prose keeps the bare names; only the SQL is quoted.
function quoteIdent(name: string) {
  return `"${name.replace(/"/g, '""')}"`
}

function sqlParams(f: SchemaLintFinding) {
  const p = f.params ?? {}
  return {
    ...textParams(f),
    schema: quoteIdent(f.schema),
    object: quoteIdent(f.object),
    column: p.column ? quoteIdent(p.column) : '',
    constraint: p.constraint ? quoteIdent(p.constraint) : '',
    index: p.index ? quoteIdent(p.index) : '',
  }
}

// Notes qualify a finding without changing it: an int4 owner column turns the
// fix from ALTER SEQUENCE into a column type change, and a nullable unique index
// means the table still has no replica identity.
function notes(f: SchemaLintFinding): string[] {
  const out: string[] = []
  const p = f.params ?? {}

  if (p.unique_nullable) out.push(t('schemaLint.notes.uniqueNullable'))
  if (p.owned_column_type && isInt4(p.owned_column_type)) {
    out.push(t('schemaLint.notes.int4Owner', { type: p.owned_column_type }))
  }
  if (p.partitions) out.push(t('schemaLint.notes.partitions', { n: p.partitions }))

  return out
}

function isInt4(type: string) {
  return ['integer', 'int4', 'int'].includes(type.trim().toLowerCase())
}

// The copy button is for fixes that are complete as written. A sequence owned by
// an int4 column is not one: ALTER SEQUENCE alone leaves the column as the real
// ceiling, and a statement offered for copying reads as "run this and you are
// done". The note and howToFix already say what the full fix costs.
function fixSql(f: SchemaLintFinding): string {
  const p = f.params ?? {}
  if (p.owned_column_type && isInt4(p.owned_column_type)) return ''

  const key = `schemaLint.checks.${f.code}.fixSql`
  return te(key) ? t(key, sqlParams(f)) : ''
}

function copyFix(f: SchemaLintFinding) {
  copyToClipboard(fixSql(f))
  copied.value = rowKey(f)
  setTimeout(() => (copied.value = null), 2000)
}

// A relation is best explored on its describe page; anything else goes to the
// section the check declared, if any.
function relatedLink(f: SchemaLintFinding) {
  if (f.object_type === 'relation') return describeLink(f.schema, f.object)
  if (!f.related_route || f.related_route === '/schema-lint') return null
  return { path: `${f.related_route}/${route.params.clustername ?? ''}`, query: route.query }
}
</script>

<template>
  <v-data-table
    :headers="headers"
    :items="rows"
    :loading="loading"
    :item-value="rowKey"
    :group-by="groupByOption"
    show-expand
  >
    <template #group-header="{ item, columns, toggleGroup, isGroupOpen }">
      <tr>
        <td :colspan="columns.length">
          <v-btn
            :icon="isGroupOpen(item) ? '$expand' : '$next'"
            size="small"
            variant="text"
            @click="toggleGroup(item)"
          />
          <span class="font-weight-medium">
            {{ groupBy === 'code' ? checkTitle(String(item.value)) : item.value }}
          </span>
          <span class="text-caption text-medium-emphasis ml-2">({{ item.items.length }})</span>
        </td>
      </tr>
    </template>

    <template #item.level="{ item }">
      <v-chip size="small" variant="tonal" :color="LEVEL_COLOR[item.level]" :prepend-icon="LEVEL_ICON[item.level]">
        {{ t(`schemaLint.levels.${item.level}`) }}
      </v-chip>
    </template>

    <template #item.code="{ item }">
      {{ checkTitle(item.code) }}
    </template>

    <template #item.qualified="{ item }">
      <component
        :is="relatedLink(item) ? 'router-link' : 'span'"
        :to="relatedLink(item) ?? undefined"
        class="text-decoration-none text-mono"
      >
        {{ item.object }}
      </component>
      <span v-if="item.params?.column" class="text-mono text-medium-emphasis">.{{ item.params.column }}</span>
      <span v-else-if="item.params?.constraint || item.params?.index" class="text-mono text-medium-emphasis">
        · {{ item.params.constraint || item.params.index }}
      </span>
      <v-chip v-if="item.params?.partitions" size="x-small" variant="tonal" class="ml-1">
        {{ t('schemaLint.table.partitions', { n: item.params.partitions }) }}
      </v-chip>
    </template>

    <template #expanded-row="{ columns, item }">
      <tr>
        <td :colspan="columns.length" class="py-3">
          <div class="mb-2">{{ checkText(item, 'description') }}</div>

          <div v-if="notes(item).length" class="mb-2">
            <div v-for="note in notes(item)" :key="note" class="text-caption text-medium-emphasis">
              <v-icon size="x-small" icon="mdi-alert-outline" class="mr-1" />{{ note }}
            </div>
          </div>

          <div class="text-body-2 mb-2">
            <span class="font-weight-medium">{{ t('schemaLint.table.howToFix') }}:</span>
            {{ checkText(item, 'howToFix') }}
          </div>

          <div v-if="fixSql(item)" class="d-flex align-center ga-2">
            <code class="text-mono sql-fix">{{ fixSql(item) }}</code>
            <v-btn
              size="x-small"
              variant="text"
              :icon="copied === rowKey(item) ? 'mdi-check' : 'mdi-content-copy'"
              :title="t('schemaLint.table.copySql')"
              @click="copyFix(item)"
            />
          </div>
        </td>
      </tr>
    </template>

    <template #no-data>
      <div class="text-center py-4 text-medium-emphasis">{{ t('schemaLint.page.noFindings') }}</div>
    </template>
  </v-data-table>
</template>

<style scoped>
.sql-fix {
  padding: 2px 6px;
  border-radius: 4px;
  background: rgba(127, 127, 127, 0.12);
}
</style>
