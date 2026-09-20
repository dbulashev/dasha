<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanFinding, PlanNode } from '@/api/models'
import { fmtBytes, fmtCompactFloat, fmtMs } from '@/utils/format'
import {
  findingText,
  findingsByPath,
  flattenPlan,
  nodeLabel,
  nodeRelation,
  nodeRows,
  nodeTimeMs,
  pathKey,
  severityColor,
} from './planText'

const props = defineProps<{
  root: PlanNode
  findings?: PlanFinding[]
  highlight?: number[] | null
}>()

const { t, te } = useI18n()

const flat = computed(() => flattenPlan(props.root))
const byPath = computed(() => findingsByPath(props.findings))

const collapsed = ref(new Set<string>())

watch(() => props.root, () => {
  collapsed.value = new Set()
})

function toggle(key: string) {
  const next = new Set(collapsed.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  collapsed.value = next
}

// The root's key is the empty string, so its children are hidden by it rather
// than by a prefix of their own key.
function hidden(key: string): boolean {
  if (key === '') return false
  if (collapsed.value.has('')) return true

  const parts = key.split('.')
  for (let i = 1; i < parts.length; i++) {
    if (collapsed.value.has(parts.slice(0, i).join('.'))) return true
  }

  return false
}

const rows = computed(() => flat.value.filter(r => !hidden(r.key)))

const highlightKey = computed(() => (props.highlight ? pathKey(props.highlight) : null))

// A finding may name a node inside a subtree the reader collapsed.
watch(highlightKey, key => {
  if (!key || collapsed.value.size === 0) return

  const next = new Set(collapsed.value)
  next.delete('')

  const parts = key.split('.')
  for (let i = 1; i < parts.length; i++) next.delete(parts.slice(0, i).join('.'))

  if (next.size !== collapsed.value.size) collapsed.value = next
})

// PostgreSQL's own labels: a merge join prints its condition and its qual as two
// separate lines, and reading them under one name would hide half the node.
function cond(node: PlanNode): { label: string; value: string; omitted?: number }[] {
  const out: { label: string; value: string; omitted?: number }[] = []
  if (node.hash_cond) out.push({ label: 'Hash Cond', value: node.hash_cond, omitted: node.hash_cond_omitted_bytes })
  if (node.merge_cond) out.push({ label: 'Merge Cond', value: node.merge_cond, omitted: node.merge_cond_omitted_bytes })
  if (node.index_cond) out.push({ label: 'Index Cond', value: node.index_cond, omitted: node.index_cond_omitted_bytes })
  if (node.recheck_cond) out.push({ label: 'Recheck Cond', value: node.recheck_cond, omitted: node.recheck_cond_omitted_bytes })
  if (node.join_filter) out.push({ label: 'Join Filter', value: node.join_filter, omitted: node.join_filter_omitted_bytes })
  if (node.filter) out.push({ label: 'Filter', value: node.filter, omitted: node.filter_omitted_bytes })
  if (node.sort_key?.length) out.push({ label: 'Sort Key', value: node.sort_key.join(', ') })
  return out
}

// Buffer counters are pages; PostgreSQL is built with 8 KiB pages unless it was
// compiled otherwise, which nothing in a plan reports.
const PAGE_BYTES = 8192

function buffersText(node: PlanNode): string {
  const b = node.buffers
  if (!b) return ''

  const parts: string[] = []
  if (b.shared_hit) parts.push(`shared hit ${fmtBytes(b.shared_hit * PAGE_BYTES)}`)
  if (b.shared_read) parts.push(`shared read ${fmtBytes(b.shared_read * PAGE_BYTES)}`)
  if (b.shared_written) parts.push(`shared written ${fmtBytes(b.shared_written * PAGE_BYTES)}`)
  if (b.local_hit) parts.push(`local hit ${fmtBytes(b.local_hit * PAGE_BYTES)}`)
  if (b.local_read) parts.push(`local read ${fmtBytes(b.local_read * PAGE_BYTES)}`)
  if (b.temp_read) parts.push(`temp read ${fmtBytes(b.temp_read * PAGE_BYTES)}`)
  if (b.temp_written) parts.push(`temp written ${fmtBytes(b.temp_written * PAGE_BYTES)}`)

  return parts.join(' · ')
}
</script>

<template>
  <div class="plan-tree text-body-2">
    <div
      v-for="row in rows"
      :key="row.key"
      class="plan-row"
      :class="{ 'plan-row-highlight': highlightKey === row.key }"
    >
      <div class="d-flex align-center flex-wrap ga-1" :style="{ paddingLeft: `${row.depth * 18}px` }">
        <v-btn
          v-if="row.node.children?.length"
          :icon="collapsed.has(row.key) ? 'mdi-chevron-right' : 'mdi-chevron-down'"
          variant="text"
          size="x-small"
          density="compact"
          @click="toggle(row.key)"
        />
        <span v-else class="plan-caret-gap"></span>

        <span class="font-weight-medium">{{ nodeLabel(row.node) }}</span>

        <span v-if="nodeRelation(row.node)" class="text-medium-emphasis">
          on <span class="text-mono">{{ nodeRelation(row.node) }}</span>
        </span>
        <span v-if="row.node.index_name" class="text-medium-emphasis">
          using <span class="text-mono">{{ row.node.index_name }}</span>
        </span>

        <v-chip
          v-for="(f, i) in byPath.get(row.key) ?? []"
          :key="i"
          :color="severityColor(f.severity)"
          size="x-small"
          variant="flat"
          label
        >
          {{ f.code }}
          <v-tooltip activator="parent" location="bottom" max-width="420">
            {{ findingText(f, t, te) }}
          </v-tooltip>
        </v-chip>

        <v-spacer />

        <span class="text-caption text-medium-emphasis plan-metrics">
          <span>
            {{ t('plan.node.cost') }}
            {{ fmtCompactFloat(row.node.startup_cost) }}..{{ fmtCompactFloat(row.node.total_cost) }}
          </span>
          <span class="ms-3">
            {{ t('plan.node.rows') }}: {{ t('plan.node.rowsPlanned') }}
            {{ fmtCompactFloat(row.node.plan_rows) }}
            <template v-if="row.node.actual">
              → {{ t('plan.node.rowsActual') }}
              <span class="font-weight-medium">{{ fmtCompactFloat(nodeRows(row.node)) }}</span>
            </template>
          </span>
          <span v-if="row.node.actual && row.node.actual.loops > 1" class="ms-3">
            × {{ fmtCompactFloat(row.node.actual.loops) }}
          </span>
          <span v-if="nodeTimeMs(row.node) != null" class="ms-3">
            {{ fmtMs(nodeTimeMs(row.node), t) }}
          </span>
        </span>
      </div>

      <div
        v-for="(c, i) in cond(row.node)"
        :key="i"
        class="text-caption text-medium-emphasis plan-cond text-mono"
        :style="{ paddingLeft: `${row.depth * 18 + 28}px` }"
      >
        {{ c.label }}: {{ c.value }}<template v-if="c.omitted">… {{ t('plan.node.omitted', { bytes: c.omitted }) }}</template>
      </div>

      <div
        v-if="row.node.rows_removed_by_filter || row.node.rows_removed_by_join_filter
          || row.node.heap_fetches || buffersText(row.node)"
        class="text-caption text-medium-emphasis plan-cond"
        :style="{ paddingLeft: `${row.depth * 18 + 28}px` }"
      >
        <span v-if="row.node.rows_removed_by_filter" class="me-3">
          {{ t('plan.node.rowsRemoved') }}: {{ fmtCompactFloat(row.node.rows_removed_by_filter) }}
        </span>
        <span v-if="row.node.rows_removed_by_join_filter" class="me-3">
          {{ t('plan.node.rowsRemovedByJoin') }}: {{ fmtCompactFloat(row.node.rows_removed_by_join_filter) }}
        </span>
        <span v-if="row.node.heap_fetches" class="me-3">
          {{ t('plan.node.heapFetches') }}: {{ fmtCompactFloat(row.node.heap_fetches) }}
        </span>
        <span v-if="buffersText(row.node)">{{ t('plan.node.buffers') }}: {{ buffersText(row.node) }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.plan-row {
  padding: 2px 0;
  border-left: 2px solid transparent;
}

.plan-row-highlight {
  border-left-color: rgb(var(--v-theme-primary));
  background: rgba(var(--v-theme-primary), 0.08);
}

.plan-caret-gap {
  display: inline-block;
  width: 24px;
}

.plan-metrics {
  white-space: nowrap;
}

.plan-cond {
  word-break: break-word;
}
</style>
