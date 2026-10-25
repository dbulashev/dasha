<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanLifecycleAnalysis, PlanLifecycleEvent } from '@/api/models'
import { causeText, fateColor, fmtCostRange, pathName, pathTypeLabel, pathsById, relAliases, relText, termText } from './planlife'

const props = defineProps<{ analysis: PlanLifecycleAnalysis; pathFilter: number | null }>()
const emit = defineEmits<{ select: [pathId: number]; 'update:pathFilter': [pathId: number | null] }>()

const { t, te } = useI18n()

const aliases = computed(() => relAliases(props.analysis))
const paths = computed(() => pathsById(props.analysis))
const relFilter = ref<string | null>(null)
const kindFilter = ref<string[]>([])

const relOptions = computed(() =>
  props.analysis.relations.map(r => ({ title: relText(r.key, aliases.value, t), value: r.key })),
)
const kindOptions = computed(() =>
  [...new Set(props.analysis.timeline.map(e => e.kind))].map(k => ({ title: termText('event', k, t, te), value: k })),
)

const events = computed(() =>
  props.analysis.timeline.filter(
    e =>
      (!relFilter.value || e.rel === relFilter.value) &&
      (!kindFilter.value.length || kindFilter.value.includes(e.kind)) &&
      (props.pathFilter == null ||
        e.path_id === props.pathFilter ||
        e.by_path_id === props.pathFilter),
  ),
)

function winnerName(id: number): string {
  const p = paths.value.get(id)
  return p ? pathName(p, aliases.value, t, true) : ''
}

// a path loses once, so its cause is the cause of the event it lost in
function lostWhy(e: PlanLifecycleEvent): string {
  if (e.path_id == null || e.by_path_id == null) return ''
  const p = paths.value.get(e.path_id)
  return p?.by === e.by_path_id ? causeText(p.cause, t) : ''
}

// event words are the same as fates apart from accepted / skipped
function kindColor(kind: string): string {
  if (kind === 'accepted') return 'info'
  if (kind === 'skipped') return 'grey'
  return fateColor(kind)
}

const headers = computed(() => [
  { title: '#', key: 'seq', width: 60 },
  { title: t('planLifecycle.col.rel'), key: 'rel' },
  { title: t('planLifecycle.col.path'), key: 'pathtype' },
  { title: t('planLifecycle.col.event'), key: 'kind' },
  { title: t('planLifecycle.col.cost'), key: 'cost_total', align: 'end' as const },
  { title: t('planLifecycle.col.link'), key: 'link', sortable: false },
])
</script>

<template>
  <div>
    <div class="d-flex flex-wrap ga-2 mb-2">
      <v-select
        v-model="relFilter"
        :items="relOptions"
        :label="t('planLifecycle.col.rel')"
        clearable
        density="compact"
        hide-details
        style="max-width: 240px"
      />
      <v-select
        v-model="kindFilter"
        :items="kindOptions"
        :label="t('planLifecycle.col.event')"
        multiple
        chips
        closable-chips
        density="compact"
        hide-details
        style="max-width: 360px"
      />
      <v-chip v-if="pathFilter != null" closable @click:close="emit('update:pathFilter', null)">
        {{ t('planLifecycle.onlyPath', { id: pathFilter }) }}
      </v-chip>
    </div>

    <v-data-table :headers="headers" :items="events" item-value="seq" density="compact" :items-per-page="50">
      <template #[`item.rel`]="{ item }">{{ relText(item.rel, aliases, t) }}</template>
      <template #[`item.pathtype`]="{ item }">
        {{ pathTypeLabel(item.pathtype) }}
        <a v-if="item.path_id != null" href="#" @click.prevent="emit('select', item.path_id)">#{{ item.path_id }}</a>
        <span v-if="item.kept_reason" class="text-caption text-medium-emphasis ms-1">
          {{ t('planLifecycle.kept', { why: termText('keptReason', item.kept_reason, t, te) }) }}
        </span>
      </template>
      <template #[`item.kind`]="{ item }">
        <v-chip :color="kindColor(item.kind)" :title="termText('eventHint', item.kind, t, te)" size="x-small" variant="flat" label>
          {{ termText('event', item.kind, t, te) }}
        </v-chip>
        <span v-if="item.skip_reason" class="text-caption ms-1">{{ termText('skipReason', item.skip_reason, t, te) }}</span>
      </template>
      <template #[`item.cost_total`]="{ item }">
        <template v-if="item.cost_total != null">
          {{ fmtCostRange(item.cost_startup, item.cost_total) }}
        </template>
      </template>
      <template #[`item.link`]="{ item }">
        <template v-if="item.by_path_id != null">
          {{ t('planLifecycle.by') }}
          <a href="#" @click.prevent="emit('select', item.by_path_id!)">
            {{ winnerName(item.by_path_id) }} #{{ item.by_path_id }}
          </a>
          <span v-if="lostWhy(item)" class="text-caption text-medium-emphasis">{{ ' · ' + lostWhy(item) }}</span>
        </template>
      </template>
    </v-data-table>
  </div>
</template>
