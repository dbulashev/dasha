<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { getMaintenanceAutovacuumQueue } from '@/api/gen/default/default'
import type {
  FeatureUnavailableReason,
  MaintenanceAutovacuumQueue,
  MaintenanceAutovacuumQueueRow,
} from '@/api/models/index'
import { PG19_VERSION_NUM } from '@/constants/pg'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useServerVersion } from '@/composables/useServerVersion'
import { assertOk, featureUnavailableReason } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtDecimal } from '@/utils/format'
import { SCORE_COMPONENTS, dominantColor, isDominant, scoreBar } from './autovacuumQueue'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const serverVersion = useServerVersion()

const PAGE_SIZES = [25, 50, 100, 500]

const page = ref(1)
const itemsPerPage = ref(50)
const queue = ref<MaintenanceAutovacuumQueue | null>(null)
const loading = ref(false)
// A 501 is explained in the card rather than hiding it: the version check
// already hides the card on hosts known to be older.
const unavailable = ref<FeatureUnavailableReason | null>(null)
// Kept in the card: the global error would replace the whole page.
const error = ref<string | null>(null)

const visible = computed(() => serverVersion.atLeast(PG19_VERSION_NUM))

let requestId = 0

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value || !visible.value) return
  const id = ++requestId
  loading.value = true
  error.value = null
  try {
    const res = await getMaintenanceAutovacuumQueue({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      limit: itemsPerPage.value,
      offset: (page.value - 1) * itemsPerPage.value,
    })
    if (id !== requestId) return
    queue.value = assertOk<MaintenanceAutovacuumQueue>(res)
  } catch (err) {
    if (id !== requestId) return
    queue.value = null
    unavailable.value = featureUnavailableReason(err, 'unsupported_version')
    if (!unavailable.value) error.value = getErrorMessage(err)
  } finally {
    if (id === requestId) loading.value = false
  }
}

watch([clusterName, hostName, databaseName], () => {
  requestId++
  unavailable.value = null
  error.value = null
  loading.value = false
  queue.value = null
  page.value = 1
})

watch([clusterName, hostName, databaseName, page, itemsPerPage, visible], load, { immediate: true })

const rows = computed<MaintenanceAutovacuumQueueRow[]>(() => queue.value?.rows ?? [])
const total = computed(() => queue.value?.total ?? 0)
const topScore = computed(() => rows.value.reduce((m, r) => Math.max(m, r.score), 0))

const headers = computed(() => [
  { title: t('header.table'), key: 'table', sortable: false },
  { title: 'score', key: 'score', sortable: false, minWidth: 140 },
  ...SCORE_COMPONENTS.map((c) => ({
    title: t(`maintenance.queue.component.${c.key}`),
    key: c.field,
    sortable: false,
    align: 'end' as const,
  })),
  { title: t('maintenance.queue.flags'), key: 'flags', sortable: false },
])

function rowProps({ item }: { item: MaintenanceAutovacuumQueueRow }) {
  return item.for_wraparound ? { class: 'bg-error-lighten' } : {}
}

const fmtScore = (v: number) => fmtDecimal(v, 2)
</script>

<template>
  <v-card v-if="visible" class="mb-4">
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <v-icon start icon="mdi-format-list-numbered" />
      <span>{{ t('maintenance.queue.title') }}</span>
      <v-tooltip :text="t('maintenance.queue.hint')" location="bottom" max-width="420">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
      <v-chip v-if="queue" size="small" variant="tonal">
        {{ t('maintenance.queue.shown', { shown: rows.length, total }) }}
      </v-chip>
    </v-card-title>
    <v-card-text>
      <v-alert v-if="unavailable === 'no_privilege'" type="info" variant="tonal" density="compact">
        {{ t('common.noPrivilege', { feature: 'pg_stat_autovacuum_scores' }) }}
      </v-alert>
      <v-alert v-else-if="unavailable" type="info" variant="tonal" density="compact">
        {{ t('common.pg19Required', { feature: 'pg_stat_autovacuum_scores' }) }}
      </v-alert>
      <v-alert v-else-if="error" type="error" variant="tonal" density="compact">{{ error }}</v-alert>
      <v-data-table-server
        v-else
        v-model:page="page"
        v-model:items-per-page="itemsPerPage"
        :items-per-page-options="PAGE_SIZES"
        :headers="headers"
        :items="rows"
        :items-length="total"
        :loading="loading"
        :row-props="rowProps"
        density="compact"
      >
        <template #item.table="{ item }">
          <span class="text-mono">{{ item.schema }}.{{ item.table }}</span>
        </template>
        <template #item.score="{ item }">
          <div class="d-flex align-center ga-2">
            <v-progress-linear
              :model-value="scoreBar(item.score, topScore)"
              :color="item.for_wraparound ? 'error' : item.score >= 1 ? 'warning' : 'primary'"
              height="6"
              rounded
              style="min-width: 60px"
            />
            <span class="text-body-2 font-weight-medium">{{ fmtScore(item.score) }}</span>
          </div>
        </template>
        <template
          v-for="c in SCORE_COMPONENTS"
          :key="c.key"
          #[`item.${c.field}`]="{ item }"
        >
          <span
            :class="isDominant(item, c.key) ? `font-weight-bold text-${dominantColor(c.key)}` : 'text-medium-emphasis'"
          >
            {{ fmtScore(item[c.field]) }}
          </span>
        </template>
        <template #item.flags="{ item }">
          <div class="d-flex ga-1">
            <v-tooltip v-if="item.do_vacuum" :text="t('maintenance.queue.doVacuum')" location="top">
              <template #activator="{ props }">
                <v-icon v-bind="props" size="small" color="warning">mdi-broom</v-icon>
              </template>
            </v-tooltip>
            <v-tooltip v-if="item.do_analyze" :text="t('maintenance.queue.doAnalyze')" location="top">
              <template #activator="{ props }">
                <v-icon v-bind="props" size="small" color="info">mdi-chart-box-outline</v-icon>
              </template>
            </v-tooltip>
            <v-tooltip v-if="item.for_wraparound" :text="t('maintenance.queue.forWraparound')" location="top" max-width="420">
              <template #activator="{ props }">
                <v-icon v-bind="props" size="small" color="error">mdi-alert-octagon</v-icon>
              </template>
            </v-tooltip>
          </div>
        </template>
      </v-data-table-server>
    </v-card-text>
  </v-card>
</template>

<style scoped>
:deep(.bg-error-lighten) {
  background-color: rgba(var(--v-theme-error), 0.08);
}
</style>
