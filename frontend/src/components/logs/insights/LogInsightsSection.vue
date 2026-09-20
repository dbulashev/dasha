<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { getLogsInsights, getLogsPlans, getLogsScan } from '@/api/gen/default/default'
import type { GetLogsServiceType, LogInsights } from '@/api/models'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { ApiError, assertOk } from '@/utils/api'
import { fmtCompact, fmtDateTime } from '@/utils/format'
import { copyToClipboard } from '@/utils/sql'
import LogFilterBar from '../LogFilterBar.vue'
import { logErrorText } from '../errors'
import type { LogFilters } from '../types'
import CategoryBreakdown from './CategoryBreakdown.vue'
import LogInsightsAbout from './LogInsightsAbout.vue'
import PlanGroupsTable from './PlanGroupsTable.vue'
import PlanRegressions from './PlanRegressions.vue'
import { planBlocker, planEmptyReason, planNotes } from './planSetup'

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()
const { clusterName, currentCluster } = useClusterInfo()

const insights = ref<LogInsights | null>(null)
const loading = ref(false)
const errorMsg = ref('')
const unavailable = ref(false)
const lastFilters = ref<LogFilters | null>(null)
const scanned = ref(false)

const initialScanId = String(route.query.scan_id ?? '')
const queryId = ref(String(route.query.query_id ?? ''))

// What the link asked for, read while the page opens: the comparison mounts with
// its tab, and by then the query string has been through the cluster selector.
const initialBaseline = {
  mode: String(route.query.baseline ?? ''),
  from: String(route.query.baseline_from ?? ''),
  to: String(route.query.baseline_to ?? ''),
}

const hosts = computed(() =>
  (currentCluster.value?.instances ?? [])
    .map(i => i.host_name)
    .filter((h): h is string => !!h),
)

const streams = computed(() => currentCluster.value?.log_streams ?? [])

const RATE_LIMIT_WAIT_SECONDS = 30
const rateLimitSeconds = ref(0)
let rateLimitTimer: ReturnType<typeof setInterval> | undefined

function startRateLimitCountdown() {
  rateLimitSeconds.value = RATE_LIMIT_WAIT_SECONDS
  clearInterval(rateLimitTimer)
  rateLimitTimer = setInterval(() => {
    rateLimitSeconds.value--
    if (rateLimitSeconds.value <= 0) clearInterval(rateLimitTimer)
  }, 1000)
}

onUnmounted(() => clearInterval(rateLimitTimer))

// scan_id and the tab travel together: two replaces in one tick would each build
// the query from a route that has not caught up with the other.
function syncUrl() {
  const rest = { ...route.query }
  delete rest.scan_id
  delete rest.tab

  const id = insights.value?.scan_id

  router.replace({
    query: {
      ...rest,
      ...(id ? { scan_id: id } : {}),
      ...(tab.value === 'plans' ? {} : { tab: tab.value }),
    },
  })
}

function setQueryId(id: string) {
  queryId.value = id
  const rest = { ...route.query }
  delete rest.query_id
  router.replace({ query: id ? { ...rest, query_id: id } : rest })
  // The narrowed read carries no categories, so the window has to be read again
  // rather than kept on screen under a filter that no longer applies.
  if (scanWindow.value) scan(scanWindow.value)
}

const scanId = computed(() => insights.value?.scan_id ?? '')

interface ScanWindow {
  serviceType: GetLogsServiceType
  from: string
  to: string
  host: string
}

// Window the results describe: a stored scan knows its own, a fresh one takes it
// from the form.
const scanWindow = computed<ScanWindow | null>(() => {
  const scan = insights.value?.scan
  if (scan) {
    return {
      serviceType: scan.service_type as GetLogsServiceType,
      from: scan.from,
      to: scan.to,
      host: scan.host ?? '',
    }
  }
  const f = lastFilters.value
  return f ? { serviceType: f.serviceType, from: f.from, to: f.to, host: f.host } : null
})

async function scan(w: ScanWindow) {
  if (!clusterName.value || loading.value) return

  loading.value = true
  errorMsg.value = ''
  rateLimitSeconds.value = 0
  clearInterval(rateLimitTimer)

  try {
    const params = {
      cluster_name: clusterName.value,
      service_type: w.serviceType,
      from: w.from,
      to: w.to,
      host: w.host || undefined,
    }
    const res = queryId.value
      ? await getLogsPlans({ ...params, query_id: queryId.value })
      : await getLogsInsights(params)

    insights.value = assertOk<LogInsights>(res)
    unavailable.value = false
    scanned.value = true
    if (!askedTab) tab.value = defaultTab.value
    syncUrl()
  } catch (err) {
    if (err instanceof ApiError && err.status === 429) {
      startRateLimitCountdown()
    } else if (err instanceof ApiError && (err.status === 404 || err.status === 501)) {
      unavailable.value = true
    } else {
      errorMsg.value = logErrorText(err, t)
    }
    insights.value = null
    scanned.value = false
  } finally {
    loading.value = false
  }
}

function onSearch(filters: LogFilters) {
  lastFilters.value = filters
  scan({ serviceType: filters.serviceType, from: filters.from, to: filters.to, host: filters.host })
}

async function loadSnapshot(id: string) {
  loading.value = true
  errorMsg.value = ''
  try {
    insights.value = assertOk<LogInsights>(await getLogsScan(id))
    scanned.value = true
  } catch (err) {
    if (err instanceof ApiError && (err.status === 404 || err.status === 501)) {
      syncUrl()
      errorMsg.value = t('logs.insights.scanGone')
    } else {
      errorMsg.value = logErrorText(err, t)
    }
    insights.value = null
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (initialScanId) loadSnapshot(initialScanId)
})

function onScanGone() {
  insights.value = null
  scanned.value = false
  syncUrl()
  errorMsg.value = t('logs.insights.scanGone')
}

type InsightsTab = 'categories' | 'plans' | 'compare'

const TABS: InsightsTab[] = ['categories', 'plans', 'compare']

const hasCategories = computed(() => !!insights.value?.categories?.length)

// A pooler writes no plans: the two plan tabs would be empty by construction.
const hasPlans = computed(() => scanWindow.value?.serviceType !== 'pooler')

const askedTab = String(route.query.tab ?? '')

const tab = ref<InsightsTab>(TABS.includes(askedTab as InsightsTab) ? (askedTab as InsightsTab) : 'plans')

// The summary answers "what is in this window" and comes first; a link and a
// narrowed read that carry no categories open on the plans.
const defaultTab = computed<InsightsTab>(() => (hasCategories.value ? 'categories' : 'plans'))

watch(tab, syncUrl)

// The tab a scan opened on may not exist in the next one.
watch([hasCategories, hasPlans], () => {
  if (!hasCategories.value && tab.value === 'categories') tab.value = 'plans'
  if (!hasPlans.value) tab.value = 'categories'
})

const partialReasons = computed(() =>
  (insights.value?.partial_reasons ?? []).map(r => t(`logs.insights.partialReason.${r}`)).join(', '),
)

const snapshotInfo = computed(() => insights.value?.scan ?? null)

const blocker = computed(() => (hasPlans.value ? planBlocker(insights.value?.configuration, t) : ''))

const emptyReason = computed(() => planEmptyReason(insights.value?.plans ?? null, t, te))

const notes = computed(() =>
  hasPlans.value ? planNotes(insights.value?.configuration, insights.value?.plans ?? null, t) : [],
)

// The blocker explains an empty result, so it is only shown with one.
const showBlocker = computed(() => !!blocker.value && (insights.value?.plans.parsed ?? 0) === 0)

function copyLink() {
  copyToClipboard(globalThis.location.href)
}
</script>

<template>
  <LogFilterBar
    mode="window"
    :hosts="hosts"
    :streams="streams"
    :loading="loading"
    :auto-submit="!initialScanId"
    :submit-label="t('logs.insights.scan')"
    @search="onSearch"
  >
    <template #meta>
      <template v-if="insights && scanned">
        <v-divider class="my-2" />
        <div class="d-flex align-center flex-wrap ga-3">
          <span class="text-caption text-medium-emphasis">
            {{ t('logs.insights.scannedRecords', { records: fmtCompact(insights.scanned) }) }}
          </span>
          <span v-if="insights.covered_from" class="text-caption text-medium-emphasis">
            {{ fmtDateTime(insights.covered_from) }} — {{ fmtDateTime(insights.covered_to) }}
          </span>
          <v-chip v-if="insights.partial" color="warning" size="small" variant="tonal" label>
            {{ t('logs.insights.partial', { reasons: partialReasons }) }}
          </v-chip>
          <v-chip
            v-for="n in insights.narrowed_by ?? []"
            :key="n"
            size="small"
            variant="tonal"
            label
            class="text-mono"
          >
            {{ n }}
          </v-chip>
          <v-chip v-if="snapshotInfo" size="small" variant="tonal" label prepend-icon="mdi-camera">
            {{ t('logs.insights.snapshotAt', { at: fmtDateTime(snapshotInfo.created_at) }) }}
          </v-chip>
          <v-spacer />
          <v-btn
            v-if="scanId && route.query.scan_id"
            size="small"
            variant="text"
            prepend-icon="mdi-link-variant"
            @click="copyLink"
          >
            {{ t('logs.insights.copyLink') }}
          </v-btn>
        </div>

        <div v-if="notes.length" class="text-caption text-medium-emphasis mt-1">
          <span v-for="(n, i) in notes" :key="i" class="me-3">{{ n }}</span>
        </div>
      </template>
    </template>
  </LogFilterBar>

  <v-alert v-if="unavailable" type="info" variant="tonal" class="mb-4">
    {{ t('logs.insights.disabled') }}
  </v-alert>

  <v-alert v-if="rateLimitSeconds > 0" type="warning" variant="tonal" class="mb-4">
    {{ t('logs.error.rateLimited', { seconds: rateLimitSeconds }) }}
  </v-alert>

  <v-alert v-if="errorMsg" type="error" variant="tonal" class="mb-4" closable @click:close="errorMsg = ''">
    {{ errorMsg }}
  </v-alert>

  <v-alert v-if="queryId" type="info" variant="tonal" density="compact" class="mb-4">
    <div class="d-flex align-center flex-wrap ga-2">
      <span>{{ t('logs.insights.queryIdFilter', { queryId }) }}</span>
      <v-spacer />
      <v-btn size="small" variant="text" @click="setQueryId('')">{{ t('logs.insights.clearQueryId') }}</v-btn>
    </div>
  </v-alert>

  <template v-if="insights && scanned">
    <v-alert v-if="showBlocker" type="warning" variant="tonal" class="mb-4">
      {{ blocker }}
    </v-alert>

    <v-alert v-if="!hasCategories && !hasPlans" type="info" variant="tonal">
      {{ emptyReason || t('logs.insights.plans.empty') }}
    </v-alert>

    <v-tabs v-if="hasCategories || hasPlans" v-model="tab" class="mb-4" color="primary">
      <v-tab v-if="hasCategories" value="categories">{{ t('logs.insights.tabs.categories') }}</v-tab>
      <v-tab v-if="hasPlans" value="plans">{{ t('logs.insights.tabs.plans') }}</v-tab>
      <v-tab v-if="hasPlans" value="compare">{{ t('logs.insights.tabs.compare') }}</v-tab>
    </v-tabs>

    <v-window v-model="tab">
      <v-window-item v-if="hasCategories" value="categories">
        <CategoryBreakdown
          :categories="insights.categories ?? []"
          :from="insights.covered_from"
          :to="insights.covered_to"
        />
      </v-window-item>

      <v-window-item v-if="hasPlans" value="plans">
        <PlanGroupsTable
          :scan-id="scanId || undefined"
          :groups="insights.plans.groups"
          :summary="insights.plans"
          :query-id="queryId || undefined"
          :empty-reason="emptyReason"
          @scan-gone="onScanGone"
        />
      </v-window-item>

      <v-window-item v-if="hasPlans" value="compare">
        <PlanRegressions
          v-if="scanWindow && clusterName"
          :cluster-name="clusterName"
          :service-type="scanWindow.serviceType"
          :from="scanWindow.from"
          :to="scanWindow.to"
          :host="scanWindow.host"
          :scan-id="scanId || undefined"
          :query-id="queryId || undefined"
          :initial-baseline="initialBaseline"
        />
      </v-window-item>
    </v-window>
  </template>

  <v-alert v-else-if="!loading && !unavailable && !errorMsg" type="info" variant="tonal" class="mb-4">
    {{ t('logs.insights.hint') }}
  </v-alert>

  <LogInsightsAbout />
</template>
