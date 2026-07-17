<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useViewError } from '@/composables/useViewError'
import { getIndexesUnusedReport } from '@/api/gen/default/default'
import type { IndexUnusedReport, IndexVerdict } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import PaginationControls from '@/components/PaginationControls.vue'
import { fmtBytes } from '@/utils/format'
import { useDescribeLink } from '@/composables/useDescribeLink'
import { usePrefsStore } from '@/stores/prefs'

const prefs = usePrefsStore()

const { clusterName, databaseName } = useClusterInfo()
const { describeLink } = useDescribeLink()
const { t } = useI18n()
const { onError } = useViewError()

const items = ref<IndexVerdict[]>([])
const unreachableHosts = ref<string[]>([])
const loading = ref(false)
const page = ref(1)
const hasMore = ref(true)

// The endpoint deliberately takes no instance: idx_scan is per-instance and not
// replicated, so only the cluster-wide picture can justify a DROP. The response
// is an object (indexes + unreachable_hosts), hence no usePaginatedApiLoader.
async function load(p = 1) {
  if (!clusterName.value || !databaseName.value) return
  loading.value = true
  try {
    const pageSize = prefs.pageSize
    const res = await getIndexesUnusedReport({
      cluster_name: clusterName.value,
      database: databaseName.value,
      limit: pageSize,
      offset: (p - 1) * pageSize,
    })
    const body = assertOk<IndexUnusedReport>(res)
    items.value = body?.indexes ?? []
    unreachableHosts.value = body?.unreachable_hosts ?? []
    page.value = p
    hasMore.value = items.value.length >= pageSize
  } catch (err) {
    onError(getErrorMessage(err), err)
    items.value = []
    unreachableHosts.value = []
  } finally {
    loading.value = false
  }
}

watch([clusterName, databaseName, () => prefs.pageSize], () => load(), { immediate: true })

const headers = computed(() => [
  { title: t('header.schema'), key: 'schema' },
  { title: t('header.table'), key: 'table' },
  { title: t('header.index'), key: 'index' },
  { title: t('header.size'), key: 'size_bytes' },
  { title: t('indexes.report.verdict'), key: 'verdict' },
  { title: t('indexes.report.reason'), key: 'reason', sortable: false },
])

const VERDICT_COLOR: Record<string, string> = {
  drop_candidate: 'success',
  used: 'info',
  stale_evidence: 'warning',
  insufficient_data: 'grey',
  unknown: 'error',
}

const VERDICT_ICON: Record<string, string> = {
  drop_candidate: 'mdi-delete-outline',
  used: 'mdi-check-circle-outline',
  stale_evidence: 'mdi-history',
  insufficient_data: 'mdi-timer-sand',
  unknown: 'mdi-help-circle-outline',
}

const fmtWindow = (days: number) => days >= 1 ? `${days.toFixed(1)} ${t('indexes.report.days')}` : `${(days * 24).toFixed(1)} ${t('time.h')}`
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-scale-balance" />{{ t('indexes.report.title') }}
      <v-tooltip :text="t('indexes.report.hint')" location="bottom" max-width="480">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <v-alert v-if="unreachableHosts.length" type="warning" variant="tonal" density="compact" class="mb-3">
        {{ t('indexes.report.unreachable', { hosts: unreachableHosts.join(', ') }) }}
      </v-alert>

      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
        item-value="index"
        show-expand
      >
        <template #item.table="{ item }">
          <router-link :to="describeLink(item.schema, item.table)" class="text-decoration-none">{{ item.table }}</router-link>
        </template>
        <template #item.index="{ item }">
          <span class="text-mono">{{ item.index }}</span>
          <v-chip v-if="item.partitioned" size="x-small" variant="tonal" class="ml-1">
            {{ t('indexes.report.partitioned', { n: item.partitions ?? 0 }) }}
          </v-chip>
        </template>
        <template #item.size_bytes="{ value }">{{ fmtBytes(value) }}</template>
        <template #item.verdict="{ item }">
          <v-chip size="small" variant="tonal" :color="VERDICT_COLOR[item.verdict]" :prepend-icon="VERDICT_ICON[item.verdict]">
            {{ t(`indexes.report.verdicts.${item.verdict}`) }}
          </v-chip>
        </template>
        <template #item.reason="{ item }">
          <span class="text-caption text-medium-emphasis">{{ item.reason }}</span>
        </template>
        <template #expanded-row="{ columns, item }">
          <tr>
            <td :colspan="columns.length" class="py-2">
              <v-table density="compact">
                <thead>
                  <tr>
                    <th>{{ t('indexes.report.host') }}</th>
                    <th>{{ t('header.indexScans') }}</th>
                    <th>{{ t('indexes.report.window') }}</th>
                    <th>{{ t('indexes.report.scansPerDay') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="h in item.per_instance" :key="h.instance">
                    <td>
                      {{ h.instance }}
                      <v-chip v-if="h.in_recovery" size="x-small" variant="tonal" class="ml-1">{{ t('indexes.report.replica') }}</v-chip>
                    </td>
                    <td>{{ h.index_scans }}</td>
                    <td>
                      {{ fmtWindow(h.window_days) }}
                      <v-tooltip v-if="!h.stats_reset_known" :text="t('indexes.report.statsResetUnknown')" location="bottom" max-width="380">
                        <template #activator="{ props }">
                          <v-icon v-bind="props" size="x-small" color="warning">mdi-alert-outline</v-icon>
                        </template>
                      </v-tooltip>
                    </td>
                    <td>{{ h.scans_per_day.toFixed(2) }}</td>
                  </tr>
                </tbody>
              </v-table>
            </td>
          </tr>
        </template>
      </v-data-table>
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
