<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Line } from 'vue-chartjs'
import {
  CategoryScale,
  Chart as ChartJS,
  Legend,
  LineElement,
  LinearScale,
  PointElement,
  Tooltip,
} from 'chart.js'
import { useI18n } from 'vue-i18n'

import { fmtIoTime } from '@/components/io/types'
import { PG19_VERSION_NUM } from '@/constants/pg'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useServerVersion } from '@/composables/useServerVersion'
import { useSnapshotsStatusStore } from '@/stores/snapshotsStatus'
import { useThemeStore } from '@/stores/theme'
import { fmtChartTime, fmtCompactFloat, fmtInt, fmtMs, fmtPct } from '@/utils/format'
import {
  fastpathExceeded,
  hasFastpath,
  lockTypeColor,
  pointValue,
  windowPartial,
  windowRows,
  windowSeconds,
  type LockMetric,
} from './lockStats'
import { LOCK_STATS_RANGES, useLockStatsHistory } from './useLockStatsHistory'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)

const { t } = useI18n()
const { clusterName, hostName } = useClusterInfo()
const serverVersion = useServerVersion()
const snapshotsStatus = useSnapshotsStatusStore()
const themeStore = useThemeStore()
const isDark = computed(() => themeStore.currentTheme() === 'dark')

snapshotsStatus.ensureLoaded()

const range = ref('1h')
const metric = ref<LockMetric>('waits')

const { history, totals, loading, unavailable, error, load, clear } = useLockStatsHistory()

// The live version decides first; the 501 reason covers a host upgraded or
// downgraded since the instance info was cached, and a version that could not
// be fetched at all.
const reason = computed(() => {
  if (serverVersion.below(PG19_VERSION_NUM)) return 'unsupported_version'
  if (snapshotsStatus.cachedAt !== null && !snapshotsStatus.available) return 'no_storage'
  return unavailable.value
})

const ready = computed(
  () => !serverVersion.pending.value && snapshotsStatus.cachedAt !== null && reason.value === null,
)

function reload() {
  if (!clusterName.value || !hostName.value) return
  if (reason.value === 'unsupported_version' || reason.value === 'no_storage') return
  if (serverVersion.pending.value || snapshotsStatus.cachedAt === null) return

  load({ clusterName: clusterName.value, hostName: hostName.value, rangeKey: range.value })
}

watch([clusterName, hostName], clear, { immediate: true })

watch(
  [
    clusterName,
    hostName,
    range,
    serverVersion.versionNum,
    serverVersion.pending,
    () => snapshotsStatus.cachedAt,
    () => snapshotsStatus.available,
  ],
  reload,
  { immediate: true },
)

const meta = computed(() => totals.value?.meta ?? history.value?.meta ?? null)
const rows = computed(() => windowRows(totals.value))
const measuredSeconds = computed(() => windowSeconds(totals.value))
const partial = computed(() => windowPartial(totals.value) || windowPartial(history.value))
const showFastpathAdvice = computed(() => fastpathExceeded(rows.value))
const empty = computed(() => !loading.value && (history.value?.series ?? []).length === 0)

const deadlockTimeout = computed(() =>
  meta.value?.deadlock_timeout_ms != null ? fmtMs(meta.value.deadlock_timeout_ms, t) : '—',
)

const spansDays = computed(() => LOCK_STATS_RANGES[range.value] > 24 * 3600)

const chartData = computed(() => {
  const series = history.value?.series ?? []
  const reference = series[0]?.points ?? []
  if (!reference.length) return null

  return {
    labels: reference.map((p) => fmtChartTime(p.to, spansDays.value)),
    datasets: series.map((s) => {
      const color = lockTypeColor(s.locktype, isDark.value)
      return {
        label: s.locktype,
        data: s.points.map((p) => pointValue(p, metric.value)),
        borderColor: color,
        backgroundColor: color,
        spanGaps: false,
        pointRadius: 0,
        borderWidth: 1.5,
        tension: 0.2,
      }
    }),
  }
})

function fmtMetric(v: number): string {
  return metric.value === 'waits' ? fmtCompactFloat(v) : fmtIoTime(v)
}

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: 'index' as const, intersect: false },
  plugins: {
    legend: { display: true, position: 'bottom' as const },
    tooltip: {
      callbacks: {
        label: (ctx: { dataset: { label?: string }; parsed: { y: number | null } }) =>
          ctx.parsed.y === null
            ? `${ctx.dataset.label ?? ''}: ${t('io.partialPoint')}`
            : `${ctx.dataset.label ?? ''}: ${fmtMetric(ctx.parsed.y)}`,
      },
    },
  },
  scales: {
    x: { ticks: { maxTicksLimit: 10, maxRotation: 0 } },
    y: {
      beginAtZero: true,
      title: {
        display: true,
        text: metric.value === 'waits' ? t('locks.stats.axisWaits') : t('locks.stats.axisWaitTime'),
      },
      ticks: { callback: (value: number) => fmtMetric(value) },
    },
  },
}))
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <span class="d-flex align-center ga-1">
        <v-icon start icon="mdi-timer-lock-outline" class="flex-shrink-0" />
        <span class="text-wrap">{{ t('locks.stats.title') }}</span>
      </span>
      <v-chip size="small" variant="tonal">{{ t('locks.stats.hostWide') }}</v-chip>
      <v-spacer />
      <template v-if="ready">
        <v-btn-toggle v-model="range" density="compact" variant="outlined" mandatory>
          <v-btn v-for="(_, key) in LOCK_STATS_RANGES" :key="key" :value="key" size="small">
            {{ t(`io.range.${key}`) }}
          </v-btn>
        </v-btn-toggle>
        <v-btn-toggle v-model="metric" density="compact" variant="outlined" mandatory>
          <v-btn value="waits" size="small">{{ t('locks.stats.metric.waits') }}</v-btn>
          <v-btn value="wait_time" size="small">{{ t('locks.stats.metric.waitTime') }}</v-btn>
        </v-btn-toggle>
        <v-btn icon="mdi-refresh" size="small" variant="text" :loading="loading" @click="reload" />
      </template>
    </v-card-title>

    <v-card-text>
      <v-alert v-if="reason === 'unsupported_version'" type="info" variant="tonal" density="compact">
        {{ t('common.pg19Required', { feature: 'pg_stat_lock' }) }}
      </v-alert>
      <v-alert v-else-if="reason === 'no_storage'" type="info" variant="tonal" density="compact">
        {{ t('common.noSnapshotStorage') }}
      </v-alert>
      <v-alert v-else-if="reason === 'no_privilege'" type="info" variant="tonal" density="compact">
        {{ t('common.noPrivilege', { feature: 'pg_stat_lock' }) }}
      </v-alert>
      <v-skeleton-loader v-else-if="!ready" type="image" height="120" />
      <v-alert v-else-if="error" type="error" variant="tonal" density="compact">{{ error }}</v-alert>

      <template v-else>
        <v-alert type="info" variant="tonal" density="compact" class="mb-3" icon="mdi-information-outline">
          {{ t('locks.stats.deadlockTimeoutHint', { value: deadlockTimeout }) }}
        </v-alert>
        <v-alert
          v-if="meta?.deadlock_timeout_changed"
          type="warning"
          variant="tonal"
          density="compact"
          class="mb-3"
        >
          {{ t('locks.stats.deadlockTimeoutChanged') }}
        </v-alert>
        <v-alert v-if="partial" type="info" variant="tonal" density="compact" class="mb-3">
          {{ t('locks.stats.partial') }}
        </v-alert>

        <v-skeleton-loader v-if="loading && !chartData" type="image" height="300" />
        <v-alert v-else-if="empty" type="info" variant="tonal" density="compact">
          {{ t('locks.stats.noSnapshots') }}
        </v-alert>
        <template v-else>
          <div v-if="chartData" class="lock-stats-chart mb-4">
            <Line :data="chartData" :options="chartOptions as any" />
          </div>

          <div class="text-caption text-medium-emphasis mb-1">
            {{ t('locks.stats.window', { window: fmtMs(measuredSeconds * 1000, t) }) }}
          </div>
          <v-table density="compact">
            <thead>
              <tr>
                <th>locktype</th>
                <th class="text-end">{{ t('locks.stats.col.waits') }}</th>
                <th class="text-end">{{ t('locks.stats.col.waitTime') }}</th>
                <th class="text-end">{{ t('locks.stats.col.avgWait') }}</th>
                <th class="text-end">{{ t('locks.stats.col.share') }}</th>
                <th class="text-end">fastpath_exceeded</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!rows.length">
                <td colspan="6" class="text-medium-emphasis">{{ t('locks.stats.noWaits') }}</td>
              </tr>
              <tr v-for="r in rows" :key="r.locktype">
                <td>
                  <span class="legend-dot me-2" :style="{ backgroundColor: lockTypeColor(r.locktype, isDark) }" />
                  <code>{{ r.locktype }}</code>
                </td>
                <td class="text-end">{{ fmtInt(r.waits) }}</td>
                <td class="text-end">{{ fmtIoTime(r.waitTimeMs) }}</td>
                <td class="text-end">{{ r.avgWaitMs == null ? '—' : fmtIoTime(r.avgWaitMs) }}</td>
                <td class="text-end">{{ r.sharePct == null ? '—' : fmtPct(r.sharePct) }}</td>
                <td class="text-end">
                  <span v-if="!hasFastpath(r.locktype) && !r.fastpathExceeded">—</span>
                  <span v-else :class="{ 'text-warning font-weight-medium': r.fastpathExceeded > 0 }">
                    {{ fmtInt(r.fastpathExceeded) }}
                  </span>
                </td>
              </tr>
            </tbody>
          </v-table>

          <v-alert v-if="showFastpathAdvice" type="warning" variant="tonal" density="compact" class="mt-3">
            {{
              t('locks.stats.fastpathAdvice', {
                value: meta?.max_locks_per_transaction ?? '—',
              })
            }}
          </v-alert>
        </template>
      </template>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.lock-stats-chart {
  width: 100%;
  height: 300px;
}

.legend-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  display: inline-block;
}
</style>
