<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { getLogsPlansCompare } from '@/api/gen/default/default'
import type { GetLogsPlansCompareServiceType, LogPlanComparison, LogPlanRegression } from '@/api/models'
import { ApiError, assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtCompact, fmtDateTime, toDateTimeInput, fromDateTimeInput, withZoneLabel } from '@/utils/format'
import SqlDialog from '@/components/queries/SqlDialog.vue'
import RegressionCard from './RegressionCard.vue'

const props = defineProps<{
  clusterName: string
  serviceType: GetLogsPlansCompareServiceType
  from: string
  to: string
  host?: string
  scanId?: string
  queryId?: string
  initialBaseline?: { mode: string; from: string; to: string }
}>()

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

type BaselineMode = 'previous' | '24h' | '7d' | 'custom'

const MODES: BaselineMode[] = ['previous', '24h', '7d', 'custom']

const asked = props.initialBaseline?.mode ?? ''
const linked = MODES.includes(asked as BaselineMode)

// A bound in the link is an instant; the fields hold wall clock in the zone the
// UI renders, the same split the log filters use.
function boundToInput(value: unknown): string {
  const raw = String(value ?? '').trim()
  if (!raw) return ''

  const at = /(?:z|[+-]\d{2}:?\d{2})$/i.test(raw)
    ? new Date(raw.replace(' ', 'T'))
    : fromDateTimeInput(raw)

  return at && !isNaN(at.getTime()) ? toDateTimeInput(at) : ''
}

function inputToBound(value: string): string {
  const at = fromDateTimeInput(value)
  return at ? at.toISOString() : ''
}

const mode = ref<BaselineMode>(linked ? (asked as BaselineMode) : 'previous')
const customFrom = ref(linked ? boundToInput(props.initialBaseline?.from) : '')
const customTo = ref(linked ? boundToInput(props.initialBaseline?.to) : '')

const modeItems = computed(() => [
  { value: 'previous', title: t('logs.insights.compare.previous') },
  { value: '24h', title: t('logs.insights.compare.dayAgo') },
  { value: '7d', title: t('logs.insights.compare.weekAgo') },
  { value: 'custom', title: t('logs.range.custom') },
])

const DAY_MS = 24 * 3600 * 1000

function baselineWindow(): { from: string; to: string } | null {
  const from = Date.parse(props.from)
  const to = Date.parse(props.to)
  if (!Number.isFinite(from) || !Number.isFinite(to)) return null

  switch (mode.value) {
    case 'previous':
      return { from: new Date(from - (to - from)).toISOString(), to: new Date(from).toISOString() }
    case '24h':
      return { from: new Date(from - DAY_MS).toISOString(), to: new Date(to - DAY_MS).toISOString() }
    case '7d':
      return { from: new Date(from - 7 * DAY_MS).toISOString(), to: new Date(to - 7 * DAY_MS).toISOString() }
    case 'custom': {
      const cf = fromDateTimeInput(customFrom.value)
      const ct = fromDateTimeInput(customTo.value)
      if (!cf || !ct || cf >= ct) return null
      return { from: cf.toISOString(), to: ct.toISOString() }
    }
  }

  return null
}

const baselinePreview = computed(() => {
  const w = baselineWindow()
  return w ? `${fmtDateTime(w.from)} — ${fmtDateTime(w.to)}` : ''
})

const result = ref<LogPlanComparison | null>(null)
const loading = ref(false)
const errorMsg = ref('')

// The comparison reads two windows and has a limiter of its own: one request a
// minute, apart from the log search.
const RATE_LIMIT_WAIT_SECONDS = 60
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

async function compare() {
  const baseline = baselineWindow()
  if (!baseline || loading.value) return

  loading.value = true
  errorMsg.value = ''
  rateLimitSeconds.value = 0
  clearInterval(rateLimitTimer)

  try {
    const res = await getLogsPlansCompare({
      cluster_name: props.clusterName,
      service_type: props.serviceType,
      ...(props.scanId ? { scan_id: props.scanId } : { from: props.from, to: props.to }),
      baseline_from: baseline.from,
      baseline_to: baseline.to,
      host: props.host || undefined,
      query_id: props.queryId || undefined,
    })
    result.value = assertOk<LogPlanComparison>(res)
  } catch (err) {
    if (err instanceof ApiError && err.status === 429) startRateLimitCountdown()
    else errorMsg.value = getErrorMessage(err)
    result.value = null
  } finally {
    loading.value = false
  }
}

function setCustomDefaults() {
  if (customFrom.value || customTo.value) return
  const w = baselineWindow()
  if (!w) return
  customFrom.value = toDateTimeInput(new Date(w.from))
  customTo.value = toDateTimeInput(new Date(w.to))
}

// The baseline lives in the URL, so a link opens the page on the comparison it
// was shared for, and runs it.
function syncQuery() {
  const rest = { ...route.query }
  delete rest.baseline
  delete rest.baseline_from
  delete rest.baseline_to

  const from = mode.value === 'custom' ? inputToBound(customFrom.value) : ''
  const to = mode.value === 'custom' ? inputToBound(customTo.value) : ''

  router.replace({
    query: {
      ...rest,
      baseline: mode.value,
      ...(from && to ? { baseline_from: from, baseline_to: to } : {}),
    },
  })
}

watch([mode, customFrom, customTo], syncQuery)

onMounted(() => {
  if (!linked) return

  // The link may have lost its baseline on the way here; put it back so the page
  // stays shareable.
  syncQuery()
  compare()
})

function regressionKey(r: LogPlanRegression, i: number): string {
  return `${i}:${r.query_id ?? r.query_text.slice(0, 80)}`
}

const sqlDialog = ref(false)
const sqlText = ref('')
const sqlQueryId = ref('')

function showSql(r: LogPlanRegression) {
  sqlQueryId.value = r.query_id ?? ''
  sqlText.value = r.query_text
  sqlDialog.value = true
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-text>
      <v-row dense align="center">
        <v-col cols="12" sm="6" md="3">
          <v-select
            v-model="mode"
            :items="modeItems"
            :label="t('logs.insights.compare.baseline')"
            density="compact"
            hide-details
            @update:model-value="setCustomDefaults"
          />
        </v-col>
        <template v-if="mode === 'custom'">
          <v-col cols="12" sm="6" md="3">
            <v-text-field
              v-model="customFrom"
              type="datetime-local"
              :label="withZoneLabel(t('logs.from'))"
              density="compact"
              hide-details
            />
          </v-col>
          <v-col cols="12" sm="6" md="3">
            <v-text-field
              v-model="customTo"
              type="datetime-local"
              :label="withZoneLabel(t('logs.to'))"
              density="compact"
              hide-details
            />
          </v-col>
        </template>
        <v-col v-else cols="12" sm="6" md="5" class="text-caption text-medium-emphasis">
          {{ baselinePreview }}
        </v-col>
        <v-spacer />
        <v-col cols="auto" class="text-right">
          <v-btn
            color="primary"
            prepend-icon="mdi-compare"
            :loading="loading"
            :disabled="!baselineWindow()"
            @click="compare"
          >
            {{ t('logs.insights.compare.run') }}
          </v-btn>
        </v-col>
      </v-row>

      <v-alert v-if="rateLimitSeconds > 0" type="warning" variant="tonal" class="mt-3">
        {{ t('logs.error.rateLimited', { seconds: rateLimitSeconds }) }}
      </v-alert>

      <v-alert v-if="errorMsg" type="error" variant="tonal" class="mt-3" closable @click:close="errorMsg = ''">
        {{ errorMsg }}
      </v-alert>

      <template v-if="result">
        <v-alert v-if="result.partial" type="warning" variant="tonal" density="compact" class="mt-3">
          {{ t('logs.insights.compare.partial') }}
        </v-alert>

        <div class="text-caption text-medium-emphasis mt-3">
          {{ t('logs.insights.compare.current') }}:
          {{ fmtDateTime(result.current.from) }} — {{ fmtDateTime(result.current.to) }}
          ({{ t('logs.insights.compare.plans', { n: fmtCompact(result.current.summary.plans.parsed) }) }})
          <template v-if="result.baseline">
            · {{ t('logs.insights.compare.baseline') }}:
            {{ fmtDateTime(result.baseline.from) }} — {{ fmtDateTime(result.baseline.to) }}
            ({{ t('logs.insights.compare.plans', { n: fmtCompact(result.baseline.summary.plans.parsed) }) }})
          </template>
        </div>

        <v-alert
          v-if="!result.baseline"
          type="info"
          variant="tonal"
          density="compact"
          class="mt-3"
        >
          {{ t('logs.insights.compare.noCurrentPlans') }}
        </v-alert>

        <template v-else>
          <RegressionCard
            v-for="(r, i) in result.regressions"
            :key="regressionKey(r, i)"
            :item="r"
            class="mt-3"
            @show-sql="showSql"
          />
        </template>

        <div v-if="result.baseline && !result.regressions.length" class="text-medium-emphasis mt-3">
          {{ t('logs.insights.compare.noRegressions') }}
        </div>
      </template>
    </v-card-text>

    <SqlDialog v-model="sqlDialog" :query-id="sqlQueryId" :sql="sqlText" />
  </v-card>
</template>

