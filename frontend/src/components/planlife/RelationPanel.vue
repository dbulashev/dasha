<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanLifecycleAnalysis, PlanLifecyclePath, PlanNode } from '@/api/models'
import { fmtCompactFloat } from '@/utils/format'
import {
  causeText,
  childIds,
  fateColor,
  matched,
  paramText,
  planNodeAt,
  pathTypeLabel,
  pathsById,
  relAliases,
  relText,
  sortPaths,
} from './planlife'

const props = defineProps<{
  analysis: PlanLifecycleAnalysis
  rel: string
  selected: number | null
}>()

const emit = defineEmits<{ select: [pathId: number] }>()

const { t } = useI18n()

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
  return childIds(p).map(childText).join(', ')
}

function childText(id: number | undefined): string {
  if (id == null) return ''
  const c = byId.value.get(id)
  return c ? `${pathTypeLabel(c.pathtype)} #${id}` : `#${id}`
}
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
          <th class="text-end">{{ t('planLifecycle.col.cost') }}</th>
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
              outer: {{ childText(p.outer_path_id) }} · inner: {{ childText(p.inner_path_id) }}
            </div>
            <div v-else-if="childIds(p).length" class="text-caption text-medium-emphasis">
              {{ t('planLifecycle.over') }} {{ childrenText(p) }}
            </div>
            <div v-if="p.in_subquery" class="text-caption text-medium-emphasis">{{ t('planLifecycle.inSubquery') }}</div>
            <div v-if="p.label" class="text-caption text-mono">{{ p.label }}</div>
          </td>
          <td>
            <v-chip :color="fateColor(p.fate)" size="x-small" variant="flat" label>
              {{ t(`planLifecycle.fate.${p.fate}`) }}
            </v-chip>
            <div v-if="p.kept_reason" class="text-caption">{{ t('planLifecycle.kept', { why: p.kept_reason }) }}</div>
            <div v-if="p.fate_reason" class="text-caption text-medium-emphasis">
              {{ t(`planLifecycle.fateReason.${p.fate_reason}`) }}
            </div>
          </td>
          <td>
            <template v-if="winner(p)">
              {{ t(p.fate === 'removed' ? 'planLifecycle.removedBy' : 'planLifecycle.lostTo') }}
              <a href="#" @click.prevent.stop="emit('select', p.by!)">
                {{ pathTypeLabel(winner(p)!.pathtype) }} #{{ p.by }}
              </a>
            </template>
            <div class="text-caption">{{ causeText(p.cause, t) }}</div>
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
              {{ p.cost_startup != null ? p.cost_startup.toFixed(2) : '—' }}..{{ p.cost_total.toFixed(2) }}
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
        {{ pathTypeLabel(g.method) }} × {{ g.count }} — {{ g.reason }},
        {{ t('planLifecycle.precheck.cost') }} {{ g.min_cost.toFixed(2) }}…{{ g.max_cost.toFixed(2) }}
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
  background: rgba(var(--v-theme-primary), 0.08);
}
</style>
