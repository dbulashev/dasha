<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getHotPercentile, getHotObjectHistory } from '@/api/gen/default/default'
import type { HotPercentile, HotObjectHistory } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { ApiError, assertOk } from '@/utils/api'
import { fmtCompact, fmtDateTime } from '@/utils/format'

const props = defineProps<{ schema: string; table: string }>()

const { clusterName, databaseName } = useClusterInfo()
const { t } = useI18n()

const HISTORY_DAYS = 30

// Silent block: 501 (no storage) and 404 (no snapshots / no anchors yet) both
// simply hide it — hotness is an enrichment, not a required describe section.
const percentiles = ref<HotPercentile[]>([])
const history = ref<HotObjectHistory | null>(null)
const loaded = ref(false)

async function load() {
  percentiles.value = []
  history.value = null
  loaded.value = false
  if (!clusterName.value || !databaseName.value || !props.schema || !props.table) return

  const base = {
    cluster_name: clusterName.value,
    database: databaseName.value,
    kind: 'table' as const,
    schema: props.schema,
    object: props.table,
  }

  try {
    const results = await Promise.all(
      (['reads', 'writes'] as const).map(cls =>
        getHotPercentile({ ...base, class: cls })),
    )
    percentiles.value = results
      .filter(r => r.status === 200)
      .map(r => assertOk<HotPercentile>(r))

    const histRes = await getHotObjectHistory({ ...base, days: HISTORY_DAYS })
    if (histRes.status === 200) {
      history.value = assertOk<HotObjectHistory>(histRes)
    }

    loaded.value = true
  } catch (err) {
    if (err instanceof ApiError && (err.status === 501 || err.status === 404)) return
    // Any other failure also just hides the block — never break describe.
  }
}

watch([clusterName, databaseName, () => props.schema, () => props.table], () => load(), { immediate: true })

const daysInTop = computed(() => {
  const items = history.value?.items ?? []
  return new Set(items.map(i => i.captured_at.slice(0, 10))).size
})

const lastTopDay = computed(() => {
  const items = history.value?.items ?? []
  return items.length ? fmtDateTime(items[0].captured_at) : null
})

// Always lead with the absolute rate: the percentile alone misleads on quiet
// databases, where trivial activity outruns an idle tail ("hotter than 90%"
// while doing 100 rows/day).
function pctLabel(p: HotPercentile): string {
  const cls = t(`hot.class.${p.class}`)
  const rate = fmtCompact(Math.round(p.rate_per_day))
  if (p.in_top) return t('hot.describe.inTop', { class: cls, rate })
  if (p.percentile > 0) {
    return t('hot.describe.hotterThan', { class: cls, rate, pct: Math.round(p.percentile * 100) })
  }
  return t('hot.describe.quiet', { class: cls, rate })
}

const show = computed(() => loaded.value && percentiles.value.length > 0)

// A fully idle table collapses to one muted line: two identical "~0/day"
// chips answer the same question with more noise. History (if any) keeps the
// full view — "was hot, quiet now" is worth the chips.
const allQuiet = computed(() =>
  percentiles.value.length > 0
  && percentiles.value.every(p => !p.in_top && Math.round(p.rate_per_day) === 0)
  && daysInTop.value === 0,
)
</script>

<template>
  <v-card v-if="show" class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-fire" /><span>{{ t('hot.describe.title') }}</span>
      <v-tooltip :text="t('hot.describe.hint')" location="bottom" max-width="420">
        <template #activator="{ props: tp }">
          <v-icon v-bind="tp" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text class="d-flex align-center ga-2 flex-wrap">
      <template v-if="allQuiet">
        <span class="text-caption text-medium-emphasis">{{ t('hot.describe.allQuiet') }}</span>
      </template>
      <template v-else>
        <v-chip
          v-for="p in percentiles"
          :key="p.class"
          size="small"
          variant="tonal"
          :color="p.in_top ? 'warning' : undefined"
          :prepend-icon="p.in_top ? 'mdi-fire' : undefined"
        >
          {{ pctLabel(p) }}
        </v-chip>
        <span v-if="daysInTop" class="text-caption text-medium-emphasis">
          {{ t('hot.describe.topAppearances', { n: daysInTop, days: HISTORY_DAYS, date: lastTopDay ?? '—' }) }}
        </span>
      </template>
    </v-card-text>
  </v-card>
</template>
