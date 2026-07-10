<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { getLogs } from '@/api/gen/default/default'
import type { GetLogsParams, LogEntry, LogSearchResult } from '@/api/models'
import { ApiError, assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { useClusterInfo } from '@/composables/useClusterInfo'
import LogFilterBar from './LogFilterBar.vue'
import LogHistogramChart from './LogHistogramChart.vue'
import LogResultsTable from './LogResultsTable.vue'
import type { LogFilters } from './types'

const { t } = useI18n()
const { clusterName, currentCluster } = useClusterInfo()

// Drill-down targets (row values, histogram buckets) call back into the
// filter bar, which owns the form state and re-submits the search.
const filterBar = ref<InstanceType<typeof LogFilterBar> | null>(null)

const items = ref<LogEntry[]>([])
const loading = ref(false)
const dedup = ref(false)
const partial = ref(false)
const scanned = ref(0)
const nextToken = ref<string | undefined>(undefined)
const errorMsg = ref<string>('')
const searched = ref(false)

const lastFilters = ref<LogFilters | null>(null)

const hosts = computed(() =>
  (currentCluster.value?.instances ?? [])
    .map(i => i.host_name)
    .filter((h): h is string => !!h),
)

const hasMore = computed(() => !dedup.value && !!nextToken.value)

const activeIncludes = computed(() => lastFilters.value?.includes ?? [])

// Dedup groups keep their count-ranked order; chronological results are sorted
// by timestamp per the selected order (newest or oldest first).
const displayItems = computed(() => {
  if (dedup.value) return items.value
  const dir = lastFilters.value?.order === 'asc' ? 1 : -1
  return [...items.value].sort(
    (a, b) => dir * ((a.timestamp ?? '') < (b.timestamp ?? '') ? -1 : (a.timestamp ?? '') > (b.timestamp ?? '') ? 1 : 0),
  )
})

// 429 shows its own alert with a countdown matching the limiter's refill
// period (1 token / 30s by default) instead of a static error message.
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

function mapError(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.status) {
      case 400:
        return t('logs.error.badRequest')
      case 404:
        return t('logs.error.notFound')
      case 501:
        return t('logs.error.unsupported')
      case 502:
        return t('logs.error.upstream')
      case 504:
        return t('logs.error.timeout')
    }
  }
  // A bare fetch rejection (not an ApiError) means no HTTP response arrived —
  // the connection dropped or the request outran the browser/proxy while the
  // backend was still waiting on Yandex Cloud. Give a clearer hint than the raw
  // "Failed to fetch".
  const msg = getErrorMessage(err)
  if (/failed to fetch|networkerror|load failed|network request failed/i.test(msg)) {
    return t('logs.error.network')
  }
  return msg
}

async function runSearch(filters: LogFilters, append: boolean) {
  if (!clusterName.value) return
  // Buttons are disabled via :loading, but guard against overlapping calls
  // (e.g. Enter in a filter field) racing to overwrite items/nextToken.
  if (loading.value) return

  loading.value = true
  errorMsg.value = ''
  rateLimitSeconds.value = 0
  clearInterval(rateLimitTimer)

  if (!append) {
    items.value = []
    nextToken.value = undefined
  }

  try {
    const params: GetLogsParams = {
      cluster_name: clusterName.value,
      service_type: filters.serviceType,
      from: filters.from,
      to: filters.to,
      severity: filters.severities.length ? filters.severities : undefined,
      host: filters.host || undefined,
      message: filters.includes.length ? filters.includes : undefined,
      exclude: filters.excludes.length ? filters.excludes : undefined,
      database: filters.database || undefined,
      user: filters.user || undefined,
      dedup: filters.dedup,
      page_size: filters.pageSize,
      page_token: append ? nextToken.value : undefined,
    }

    const res = assertOk(await getLogs(params)) as LogSearchResult

    items.value = append ? [...items.value, ...res.items] : res.items
    dedup.value = res.dedup
    partial.value = res.partial
    scanned.value = res.scanned ?? 0
    nextToken.value = res.next_page_token
    lastFilters.value = filters
    searched.value = true
  } catch (err) {
    if (err instanceof ApiError && err.status === 429) {
      startRateLimitCountdown()
    } else {
      errorMsg.value = mapError(err)
    }
    if (!append) {
      // A fresh search failed: drop the previous search's result state so
      // stale "no results" / "scan limit" alerts don't render next to the error.
      searched.value = false
      dedup.value = false
      partial.value = false
      scanned.value = 0
    }
  } finally {
    loading.value = false
  }
}

function onSearch(filters: LogFilters) {
  runSearch(filters, false)
}

function onLoadMore() {
  if (lastFilters.value) runSearch(lastFilters.value, true)
}

function onDrill(field: 'severity' | 'user' | 'database' | 'host', value: string) {
  filterBar.value?.applyDrill(field, value)
}

function onExclude(text: string) {
  filterBar.value?.addExclude(text)
}

function onZoom(fromIso: string, toIso: string) {
  filterBar.value?.applyAbsoluteRange(fromIso, toIso)
}

// Back-to-top button: after paging through many rows with "load more" the page
// gets long, so offer a quick jump back to the filters.
const showScrollTop = ref(false)

function onScroll() {
  showScrollTop.value = window.scrollY > 600
}

function scrollToTop() {
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

onMounted(() => window.addEventListener('scroll', onScroll, { passive: true }))
onUnmounted(() => {
  window.removeEventListener('scroll', onScroll)
  clearInterval(rateLimitTimer)
})
</script>

<template>
  <LogFilterBar ref="filterBar" :hosts="hosts" :loading="loading" @search="onSearch" />

  <v-alert
    v-if="rateLimitSeconds > 0"
    type="warning"
    variant="tonal"
    class="mb-4"
    closable
    @click:close="rateLimitSeconds = 0"
  >
    {{ t('logs.error.rateLimited', { seconds: rateLimitSeconds }) }}
  </v-alert>

  <v-alert
    v-if="errorMsg"
    type="error"
    variant="tonal"
    class="mb-4"
    closable
    @click:close="errorMsg = ''"
  >
    {{ errorMsg }}
  </v-alert>

  <!-- Dedup groups carry no per-record timestamps, so a frequency chart is only
       meaningful for chronological results. -->
  <LogHistogramChart v-if="searched && !dedup && items.length" :items="items" @zoom="onZoom" />

  <LogResultsTable
    :items="displayItems"
    :loading="loading"
    :dedup="dedup"
    :partial="partial"
    :scanned="scanned"
    :has-more="hasMore"
    :messages="activeIncludes"
    :searched="searched"
    @load-more="onLoadMore"
    @filter="onDrill"
    @exclude="onExclude"
  />

  <v-fade-transition>
    <v-btn
      v-show="showScrollTop && items.length > 0"
      icon="mdi-chevron-up"
      color="primary"
      elevation="6"
      class="log-scroll-top"
      :title="t('logs.scrollTop')"
      @click="scrollToTop"
    />
  </v-fade-transition>
</template>

<style scoped>
.log-scroll-top {
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: 1000;
}
</style>
