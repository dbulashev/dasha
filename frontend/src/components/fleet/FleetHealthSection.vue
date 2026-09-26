<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { getHealthScoreFleet } from '@/api/gen/default/default'
import type { HealthScoreFleet, HealthScoreFleetItem } from '@/api/models/index'
import { useApiLoader } from '@/composables/useApiLoader'
import { useClustersStore } from '@/stores/clusters'
import { usePrefsStore } from '@/stores/prefs'
import { fmtMs } from '@/utils/format'
import { scoreColor } from '@/components/health-score/scoreColor'

const FLEET_MAX_LIMIT = 50

const { t } = useI18n()
const router = useRouter()
const clusterStore = useClustersStore()
const prefs = usePrefsStore()

const page = ref(1)
const itemsPerPage = ref(prefs.pageSize)
const clusters = ref<string[]>([])
const appliedClusters = ref<string[]>([])
const error = ref<string | null>(null)

const clusterNames = computed(() =>
  (clusterStore.clusterList?.map((c) => c.name).filter(Boolean) as string[] ?? []).sort(),
)

const { items: data, loading } = useApiLoader<HealthScoreFleet | null>(
  () => {
    error.value = null
    page.value = 1
    return getHealthScoreFleet({
      limit: FLEET_MAX_LIMIT,
      ...(appliedClusters.value.length ? { cluster_name: appliedClusters.value } : {}),
    })
  },
  {
    deps: [appliedClusters],
    guard: () => true,
    onError: (msg) => {
      error.value = msg
    },
    defaultValue: null,
  },
)

function analyze() {
  appliedClusters.value = [...clusters.value]
}

const items = computed<HealthScoreFleetItem[]>(() => {
  const list = data.value?.items
  return Array.isArray(list) ? list : []
})

const headers = computed(() => [
  { title: t('fleetHealth.headers.cluster'), key: 'cluster_name', sortable: false },
  { title: t('fleetHealth.headers.instance'), key: 'instance', sortable: false },
  { title: t('fleetHealth.headers.score'), key: 'score', sortable: false },
  { title: t('fleetHealth.headers.source'), key: 'source', sortable: false },
  { title: t('fleetHealth.headers.role'), key: 'in_recovery', sortable: false },
  { title: t('fleetHealth.headers.error'), key: 'error', sortable: false },
])

function roleText(item: HealthScoreFleetItem): string {
  if (item.source === 'none') return '—'
  return item.in_recovery ? t('hostRole.replica') : t('hostRole.primary')
}

function openCard(item: HealthScoreFleetItem) {
  router.push({
    name: 'HealthScore',
    params: { clustername: item.cluster_name },
    query: { host: item.instance },
  })
}
</script>

<template>
  <v-card>
    <v-card-text class="d-flex flex-column ga-3">
      <div class="d-flex align-center flex-wrap ga-3">
        <v-autocomplete
          v-model="clusters"
          :items="clusterNames"
          :label="t('fleetHealth.clusters')"
          :placeholder="t('fleetHealth.allClusters')"
          persistent-placeholder
          multiple
          chips
          closable-chips
          clearable
          density="compact"
          variant="outlined"
          hide-details
          autocomplete="off"
          class="fleet-clusters"
        />

        <v-btn color="primary" variant="tonal" :loading="loading" @click="analyze">
          {{ t('fleetHealth.analyze') }}
        </v-btn>

        <v-spacer />

        <span v-if="data?.duration_ms != null" class="text-caption text-medium-emphasis">
          {{ t('fleetHealth.computedIn', { duration: fmtMs(data.duration_ms, t) }) }}
        </span>
      </div>

      <v-alert v-if="error" type="error" variant="tonal" density="compact" :text="error" />

      <template v-if="data">
        <v-alert
          v-if="data.incomplete"
          type="warning"
          variant="tonal"
          density="compact"
          icon="mdi-timer-sand"
          :text="
            t('fleetHealth.incomplete', {
              scored: data.instances_scored,
              total: data.instances_total,
              uncomputed: data.uncomputed,
            })
          "
        />
        <v-alert
          v-if="data.metrics_unavailable"
          type="warning"
          variant="tonal"
          density="compact"
          icon="mdi-chart-line-variant"
          :text="t('fleetHealth.metricsUnavailable')"
        />
      </template>

      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading && !items.length"
        :no-data-text="t('fleetHealth.empty')"
        v-model:page="page"
        v-model:items-per-page="itemsPerPage"
        density="compact"
        hover
        class="fleet-table"
        @click:row="(_: unknown, { item }: { item: HealthScoreFleetItem }) => openCard(item)"
      >
        <template #item.score="{ item }">
          <v-chip
            v-if="item.score != null"
            :color="scoreColor(item.score)"
            variant="flat"
            size="small"
          >
            {{ Math.round(item.score) }}
          </v-chip>
          <span v-else class="text-medium-emphasis">—</span>
        </template>
        <template #item.source="{ item }">
          <span class="d-inline-flex align-center ga-1">
            {{ t(`fleetHealth.source.${item.source}`) }}
            <v-tooltip v-if="item.metrics_degraded" :text="t('fleetHealth.metricsDegraded')" location="bottom" max-width="360">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="small" color="warning">mdi-alert</v-icon>
              </template>
            </v-tooltip>
          </span>
        </template>
        <template #item.in_recovery="{ item }">
          {{ roleText(item) }}
        </template>
        <template #item.error="{ item }">
          <span v-if="item.error" class="text-error text-caption">{{ item.error }}</span>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.fleet-clusters {
  min-width: 240px;
  max-width: 480px;
}

.fleet-table :deep(tbody tr) {
  cursor: pointer;
}
</style>
