<script setup lang="ts">
import { computed, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanLifecycleAnalysis, PlanLifecyclePath, PlanNode } from '@/api/models'
import { fmtCompactFloat } from '@/utils/format'
import {
  causeText,
  childIds,
  fateColor,
  fmtCost,
  fmtCostRange,
  fmtRatio,
  matched,
  paramText,
  pathName,
  planNodeAt,
  pathTypeLabel,
  pathsById,
  relAliases,
  relText,
  sortPaths,
  termText,
} from './planlife'

const props = defineProps<{
  analysis: PlanLifecycleAnalysis
  rel: string
  selected: number | null
}>()

const emit = defineEmits<{ select: [pathId: number] }>()

const { t, te } = useI18n()

const byId = computed(() => pathsById(props.analysis))
const planRoot = computed(() => (matched(props.analysis) ? props.analysis.plan?.root : undefined))

function planNode(p: PlanLifecyclePath): PlanNode | undefined {
  return planNodeAt(planRoot.value, p.plan_node)
}
const aliases = computed(() => relAliases(props.analysis))
const relation = computed(() => props.analysis.relations.find(r => r.key === props.rel))

const paths = computed(() =>
  sortPaths((relation.value?.path_ids ?? []).map(id => byId.value.get(id)).filter((p): p is PlanLifecyclePath => !!p)),
)

// precheck_no_inputs is said by the precheck hint itself
const caveats = computed(() =>
  props.analysis.caveats.filter(
    c =>
      (c.scope === 'relation' && c.rel === props.rel && c.code !== 'precheck_no_inputs') ||
      (c.scope === 'path' && c.path_id != null && relation.value?.path_ids.includes(c.path_id)),
  ),
)

function winner(p: PlanLifecyclePath): PlanLifecyclePath | undefined {
  return p.by != null ? byId.value.get(p.by) : undefined
}

function childrenText(p: PlanLifecyclePath): string {
  return childIds(p).map(inputText).join(', ')
}

function inputText(id: number | undefined): string {
  const c = id != null ? byId.value.get(id) : undefined
  return c ? pathName(c, aliases.value, t) : id != null ? `#${id}` : ''
}

function childText(id: number | undefined): string {
  if (id == null) return ''
  const c = byId.value.get(id)
  return c ? `${pathName(c, aliases.value, t)} #${id}` : `#${id}`
}

// the chosen path of the relation, else its cheapest survivor; parameterized costs are per loop and do not compare
const reference = computed(() => {
  const own = paths.value.filter(p => p.cost_total != null && !p.required_outer?.length)
  return own.find(p => p.fate === 'chosen') ?? own.find(p => p.fate === 'survived')
})

const LOST = new Set(['displaced', 'removed', 'rejected'])

function ratioText(p: PlanLifecyclePath): string {
  const ref = reference.value
  if (!ref?.cost_total || !LOST.has(p.fate) || p.cost_total == null || p.required_outer?.length) return ''
  return fmtRatio(p.cost_total / ref.cost_total)
}

// a path picked elsewhere (the summary, a chain) may be below the fold
watch(
  () => props.selected,
  () => nextTick(() => document.querySelector('.planlife-selected')?.scrollIntoView({ block: 'center', behavior: 'smooth' })),
)
</script>

<template>
  <div v-if="relation">
    <div class="d-flex align-center flex-wrap ga-2 mb-2">
      <span class="text-subtitle-1 font-weight-medium">{{ relText(rel, aliases, t) }}</span>
      <span class="text-caption text-medium-emphasis text-mono">{{ rel }}</span>
      <v-chip
        v-for="(n, fate) in relation.counts"
        :key="fate"
        :color="fateColor(String(fate))"
        size="x-small"
        variant="tonal"
        label
      >
        {{ t(`planLifecycle.fate.${fate}`) }}: {{ n }}
      </v-chip>
    </div>

    <v-table density="compact" class="text-body-2">
      <thead>
        <tr>
          <th>{{ t('planLifecycle.col.path') }}</th>
          <th>{{ t('planLifecycle.col.fate') }}</th>
          <th>{{ t('planLifecycle.col.why') }}</th>
          <th class="text-end">{{ t('planLifecycle.col.rows') }}</th>
          <th class="text-end" :title="reference ? t('planLifecycle.ratioHint', { path: `#${reference.path_id}` }) : undefined">
            {{ t('planLifecycle.col.cost') }}
          </th>
          <th>{{ t('planLifecycle.col.events') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="p in paths"
          :key="p.path_id"
          :class="{ 'planlife-selected': p.path_id === selected }"
          class="planlife-row"
          @click="emit('select', p.path_id)"
        >
          <td>
            <div class="font-weight-medium">{{ pathTypeLabel(p.pathtype) }} <span class="text-medium-emphasis">#{{ p.path_id }}</span></div>
            <div v-if="p.index_name" class="text-caption">
              {{ t('planLifecycle.usingIndex') }} <span class="text-mono">{{ p.index_name }}</span>
            </div>
            <div v-else-if="p.hypothetical_index" class="text-caption">{{ t('planLifecycle.hypotheticalIndex') }}</div>
            <div v-if="paramText(p, aliases)" class="text-caption">
              {{ t('planLifecycle.paramFrom', { rels: paramText(p, aliases) }) }}
            </div>
            <div v-if="p.outer_path_id != null" class="text-caption text-medium-emphasis">
              {{ t('planLifecycle.outer') }}: {{ inputText(p.outer_path_id) }} · {{ t('planLifecycle.inner') }}: {{ inputText(p.inner_path_id) }}
            </div>
            <div v-else-if="childIds(p).length" class="text-caption text-medium-emphasis">
              {{ t('planLifecycle.over') }} {{ childrenText(p) }}
            </div>
            <div v-if="p.in_subquery" class="text-caption text-medium-emphasis">{{ t('planLifecycle.inSubquery') }}</div>
            <div v-if="p.label" class="text-caption text-mono">{{ p.label }}</div>
          </td>
          <td>
            <v-chip :color="fateColor(p.fate)" :title="t(`planLifecycle.fateHint.${p.fate}`)" size="x-small" variant="flat" label>
              {{ t(`planLifecycle.fate.${p.fate}`) }}
            </v-chip>
            <div v-if="p.kept_reason" class="text-caption">
              {{ t('planLifecycle.kept', { why: termText('keptReason', p.kept_reason, t, te) }) }}
            </div>
            <div v-if="p.fate_reason" class="text-caption text-medium-emphasis">
              {{ t(`planLifecycle.fateReason.${p.fate_reason}`) }}
            </div>
          </td>
          <td>
            <template v-if="winner(p)">
              {{ t(p.fate === 'removed' ? 'planLifecycle.removedBy' : 'planLifecycle.lostTo') }}
              <a href="#" @click.prevent.stop="emit('select', p.by!)">{{ childText(p.by) }}</a>
            </template>
            <div class="text-caption">{{ causeText(p.cause, t) }}</div>
            <div v-if="p.fate === 'child_only' && p.parents.length" class="text-caption">
              {{ t('planLifecycle.inputOf') }}
              <template v-for="(id, i) in p.parents" :key="id">
                <a href="#" @click.prevent.stop="emit('select', id)">{{ childText(id) }}</a>{{ i < p.parents.length - 1 ? ', ' : '' }}
              </template>
            </div>
          </td>
          <td class="text-end">
            {{ fmtCompactFloat(p.rows) }}
            <div v-if="p.required_outer?.length" class="text-caption text-medium-emphasis">
              {{ t('planLifecycle.perLoop') }}
            </div>
            <div v-if="planNode(p)?.actual" class="text-caption">
              {{ t('planLifecycle.actualRows', { rows: fmtCompactFloat(planNode(p)!.actual!.rows) }) }}
              <template v-if="planNode(p)!.actual!.loops > 1"> × {{ fmtCompactFloat(planNode(p)!.actual!.loops) }}</template>
            </div>
            <v-chip v-if="p.rows_ratio != null" color="error" size="x-small" variant="tonal" label>
              {{ t('planLifecycle.rowsRatio', { ratio: fmtCompactFloat(p.rows_ratio) }) }}
            </v-chip>
          </td>
          <td class="text-end text-no-wrap">
            <template v-if="p.cost_total != null">
              <span :title="`${p.cost_startup ?? '—'}..${p.cost_total}`">{{ fmtCostRange(p.cost_startup, p.cost_total) }}</span>
              <div v-if="ratioText(p)" class="text-caption text-medium-emphasis">{{ ratioText(p) }}</div>
            </template>
            <span v-else class="text-medium-emphasis">{{ t('planLifecycle.noOwnEvents') }}</span>
          </td>
          <td class="text-caption text-mono">{{ p.events.map(s => `#${s}`).join(' ') }}</td>
        </tr>
      </tbody>
    </v-table>

    <div v-if="relation.precheck.length" class="mt-3">
      <div class="text-subtitle-2">{{ t('planLifecycle.precheck.title') }}</div>
      <div class="text-caption text-medium-emphasis mb-1">{{ t('planLifecycle.precheck.hint') }}</div>
      <div v-for="g in relation.precheck" :key="g.method + g.reason" class="text-body-2">
        {{ pathTypeLabel(g.method) }} × {{ g.count }} — {{ termText('skipReason', g.reason, t, te) }},
        {{ t('planLifecycle.precheck.cost') }} {{ fmtCost(g.min_cost) }}…{{ fmtCost(g.max_cost) }}
      </div>
    </div>

    <v-alert
      v-for="(c, i) in caveats"
      :key="i"
      type="info"
      variant="tonal"
      density="compact"
      class="mt-2"
    >
      <span v-if="c.path_id != null" class="text-mono">#{{ c.path_id }}: </span>
      {{ t(`planLifecycle.caveat.${c.code}`, c.params ?? {}) }}
    </v-alert>
  </div>
</template>

<style scoped>
.planlife-row {
  cursor: pointer;
}

.planlife-selected {
  background: rgba(var(--v-theme-primary), 0.18);
  box-shadow: inset 3px 0 0 rgb(var(--v-theme-primary));
}
</style>
