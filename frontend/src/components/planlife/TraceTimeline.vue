<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanLifecycleAnalysis } from '@/api/models'
import { fateColor, pathTypeLabel, relAliases, relText } from './planlife'

const props = defineProps<{ analysis: PlanLifecycleAnalysis; pathFilter: number | null }>()
const emit = defineEmits<{ select: [pathId: number]; 'update:pathFilter': [pathId: number | null] }>()

const { t } = useI18n()

const aliases = computed(() => relAliases(props.analysis))
const relFilter = ref<string | null>(null)
const kindFilter = ref<string[]>([])

const relOptions = computed(() =>
  props.analysis.relations.map(r => ({ title: relText(r.key, aliases.value, t), value: r.key })),
)
const kindOptions = computed(() => [...new Set(props.analysis.timeline.map(e => e.kind))])

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
        <span v-if="item.kept_reason" class="text-caption text-medium-emphasis"> {{ t('planLifecycle.kept', { why: item.kept_reason }) }}</span>
      </template>
      <template #[`item.kind`]="{ item }">
        <v-chip :color="kindColor(item.kind)" size="x-small" variant="flat" label>{{ item.kind }}</v-chip>
        <span v-if="item.skip_reason" class="text-caption ms-1">{{ item.skip_reason }}</span>
      </template>
      <template #[`item.cost_total`]="{ item }">
        <template v-if="item.cost_total != null">
          {{ item.cost_startup != null ? item.cost_startup.toFixed(2) + '..' : '' }}{{ item.cost_total.toFixed(2) }}
        </template>
      </template>
      <template #[`item.link`]="{ item }">
        <template v-if="item.by_path_id != null">
          {{ t('planLifecycle.by') }}
          <a href="#" @click.prevent="emit('select', item.by_path_id!)">#{{ item.by_path_id }}</a>
          <span v-if="item.by_seq" class="text-medium-emphasis"> (#{{ item.by_seq }})</span>
        </template>
        <span v-if="item.gap != null" class="text-caption text-medium-emphasis ms-1">gap {{ item.gap.toFixed(2) }}</span>
      </template>
    </v-data-table>
  </div>
</template>
