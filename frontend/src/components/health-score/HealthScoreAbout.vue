<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t, te } = useI18n()

const CATEGORIES = [
  { name: 'connections', weight: 0.15, icon: 'mdi-lan-connect' },
  { name: 'performance', weight: 0.15, icon: 'mdi-speedometer' },
  { name: 'storage', weight: 0.1, icon: 'mdi-database' },
  { name: 'replication', weight: 0.15, icon: 'mdi-content-copy' },
  { name: 'maintenance', weight: 0.15, icon: 'mdi-wrench' },
  { name: 'horizon', weight: 0.1, icon: 'mdi-axis-arrow' },
  { name: 'wal_checkpoint', weight: 0.1, icon: 'mdi-file-document-edit' },
  { name: 'locks', weight: 0.1, icon: 'mdi-lock-outline' },
] as const

// Penalty breakpoints. `metric` is a SQL-like identifier (language-neutral);
// `pointsKey` looks up the localized breakpoint string from i18n.
const PENALTY_ROWS = [
  { cat: 'connections', metric: 'total / max_connections', pointsKey: 'connection_ratio' },
  { cat: 'connections', metric: 'idle_in_transaction', pointsKey: 'idle_in_tx' },
  { cat: 'connections', metric: 'longest_transaction_seconds', pointsKey: 'longest_transaction' },
  { cat: 'performance', metric: 'cache_hit_ratio (%)', pointsKey: 'cache_hit_ratio' },
  { cat: 'performance', metric: 'track_io_timing', pointsKey: 'track_io_timing' },
  { cat: 'storage', metric: 'max_dead_ratio (%)', pointsKey: 'max_dead_ratio' },
  { cat: 'storage', metric: 'avg_dead_ratio (%)', pointsKey: 'avg_dead_ratio' },
  { cat: 'storage', metric: 'tables_high_bloat', pointsKey: 'tables_high_bloat' },
  { cat: 'storage', metric: 'hot_update_ratio', pointsKey: 'hot_update_ratio' },
  { cat: 'storage', metric: 'newpage_update_ratio', pointsKey: 'newpage_update_ratio' },
  { cat: 'replication', metric: 'max_replay_lag_seconds', pointsKey: 'max_replay_lag' },
  { cat: 'replication', metric: 'max_lag_bytes', pointsKey: 'max_lag_bytes' },
  { cat: 'replication', metric: 'disconnected_replicas', pointsKey: 'disconnected_replicas' },
  { cat: 'maintenance', metric: 'max(xid_age, relfrozenxid_age)', pointsKey: 'max_xid_age' },
  { cat: 'maintenance', metric: 'vacuum_backlog_tables', pointsKey: 'vacuum_backlog_tables' },
  { cat: 'maintenance', metric: 'max_overdue_vacuum_age_hours', pointsKey: 'max_overdue_vacuum_age_hours' },
  { cat: 'maintenance', metric: 'tables_never_vacuumed', pointsKey: 'tables_never_vacuumed' },
  { cat: 'maintenance', metric: 'tables_with_autovacuum_off', pointsKey: 'tables_with_autovacuum_off' },
  { cat: 'maintenance', metric: 'stale_planner_stats_tables', pointsKey: 'stale_planner_stats' },
  { cat: 'maintenance', metric: 'autovacuum / track_counts', pointsKey: 'autovacuum_track_counts' },
  { cat: 'horizon', metric: 'horizon_lag_xids', pointsKey: 'horizon_lag_xids' },
  { cat: 'wal_checkpoint', metric: 'requested / total_checkpoints', pointsKey: 'requested_checkpoint_ratio' },
  { cat: 'wal_checkpoint', metric: 'wal_level', pointsKey: 'wal_level_mismatch' },
  { cat: 'locks', metric: 'penaltyLocks', pointsKey: 'penalty_locks' },
] as const

// Rule order within a category — must match Registry. Threshold strings come
// from i18n via ruleThresholds(id) to avoid embedding RU/DE units in code.
const RULES_BY_CATEGORY: Record<string, { id: string }[]> = {
  connections: [
    { id: 'high_connection_ratio' },
    { id: 'idle_in_transaction' },
    { id: 'long_running_transaction' },
    { id: 'host_cpu_saturation' },
    { id: 'pooler_saturation' },
  ],
  performance: [
    { id: 'low_cache_hit_ratio' },
    { id: 'track_io_timing_disabled' },
    { id: 'latency_regression' },
    { id: 'seq_scan_regression' },
  ],
  storage: [
    { id: 'high_max_dead_ratio' },
    { id: 'high_avg_dead_ratio' },
    { id: 'many_bloated_tables' },
    { id: 'low_hot_update_ratio' },
    { id: 'high_newpage_update_ratio' },
    { id: 'checksum_failures' },
    { id: 'sequence_exhaustion' },
    { id: 'host_disk_space' },
  ],
  replication: [
    { id: 'replication_lag_time' },
    { id: 'replication_lag_bytes' },
    { id: 'disconnected_replicas' },
  ],
  maintenance: [
    { id: 'xid_wraparound_risk' },
    { id: 'stale_vacuum' },
    { id: 'vacuum_backlog' },
    { id: 'tables_never_vacuumed' },
    { id: 'autovacuum_disabled' },
    { id: 'track_counts_disabled' },
    { id: 'tables_with_autovacuum_off' },
    { id: 'relfrozenxid_age_outlier' },
    { id: 'stale_planner_stats' },
  ],
  horizon: [{ id: 'horizon_lag_xids' }],
  wal_checkpoint: [
    { id: 'requested_checkpoint_ratio' },
    { id: 'wal_level_minimal_with_replicas' },
    { id: 'wal_level_logical_without_publications' },
  ],
  locks: [
    { id: 'active_lock_waiters' },
    { id: 'longest_lock_wait_seconds' },
    { id: 'ungranted_locks' },
    { id: 'deadlocks_rate' },
    { id: 'lock_pool_saturation' },
  ],
}

const categoryIcon = (name: string): string =>
  CATEGORIES.find((c) => c.name === name)?.icon ?? 'mdi-circle'

const categoryLabel = (name: string): string => t(`healthScore.categories.${name}`)

const ruleTitle = (id: string): string => {
  const k = `healthScore.recommendations.${id}.title`
  return te(k) ? t(k) : id
}

const ruleNote = (id: string): string => {
  const k = `healthScore.about.ruleNotes.${id}`
  return te(k) ? t(k) : ''
}

const penaltyPoints = (key: string): string =>
  t(`healthScore.about.penaltyPoints.${key}`)

const ruleThresholds = (id: string): string =>
  t(`healthScore.about.ruleThresholds.${id}`)
</script>

<template>
  <v-expansion-panels variant="accordion" class="mb-4">
    <v-expansion-panel>
      <v-expansion-panel-title color="surface">
        <template #default="{ expanded }">
          <span class="d-flex align-center ga-2">
            <v-icon size="small">mdi-{{ expanded ? 'book-open-variant' : 'book-outline' }}</v-icon>
            <span class="text-subtitle-1 font-weight-medium">
              {{ t('healthScore.about.toggle') }}
            </span>
          </span>
        </template>
      </v-expansion-panel-title>

      <v-expansion-panel-text>
        <!-- Intro + Formula -->
        <section class="about-section">
          <p class="text-body-1 mb-3">{{ t('healthScore.about.intro') }}</p>

          <v-card variant="tonal" color="primary" class="mb-3">
            <v-card-text class="pa-3">
              <div class="text-overline mb-1">{{ t('healthScore.about.formulaTitle') }}</div>
              <pre class="formula-code mb-2"
>score = 100 − Σ (penalty<sub>i</sub> × weight<sub>i</sub>)
clamp(0 … 100)</pre>
              <div class="text-body-2 text-medium-emphasis">
                {{ t('healthScore.about.formulaDescription') }}
              </div>
            </v-card-text>
          </v-card>

          <v-alert
            type="info"
            variant="tonal"
            density="compact"
            border="start"
            icon="mdi-puzzle-remove"
          >
            <div class="font-weight-medium mb-1">{{ t('healthScore.about.dropTitle') }}</div>
            <div class="text-body-2 mb-2">{{ t('healthScore.about.dropIntro') }}</div>
            <ul class="text-body-2 ms-4">
              <li>{{ t('healthScore.about.dropReplication') }}</li>
              <li>{{ t('healthScore.about.dropMaintenance') }}</li>
            </ul>
          </v-alert>

          <v-alert
            type="error"
            variant="tonal"
            density="compact"
            border="start"
            icon="mdi-fire"
            class="mt-3"
          >
            <div class="font-weight-medium mb-1">{{ t('healthScore.about.criticalTitle') }}</div>
            <div class="text-body-2 mb-2">{{ t('healthScore.about.criticalIntro') }}</div>
            <ul class="text-body-2 ms-4">
              <li>{{ t('healthScore.about.criticalXid') }}</li>
              <li>{{ t('healthScore.about.criticalAutovacuum') }}</li>
              <li>{{ t('healthScore.about.criticalTrackCounts') }}</li>
            </ul>
          </v-alert>

          <p class="text-body-2 text-medium-emphasis mt-3">
            {{ t('healthScore.about.rulesEngine') }}
          </p>
        </section>

        <v-divider class="my-4" />

        <!-- Categories -->
        <section class="about-section">
          <h3 class="text-h6 mb-3 d-flex align-center ga-2">
            <v-icon size="small">mdi-shape</v-icon>
            {{ t('healthScore.about.categoriesTitle') }}
          </h3>

          <v-row dense>
            <v-col v-for="cat in CATEGORIES" :key="cat.name" cols="12" sm="6" md="4">
              <v-card variant="outlined" class="h-100">
                <v-card-text class="d-flex align-start ga-2 pa-3">
                  <v-icon size="small" class="mt-1">{{ cat.icon }}</v-icon>
                  <div class="flex-grow-1">
                    <div class="d-flex align-center justify-space-between">
                      <span class="text-body-2 font-weight-medium">
                        {{ categoryLabel(cat.name) }}
                      </span>
                      <v-chip size="x-small" variant="tonal" density="compact">
                        {{ cat.weight.toFixed(2) }}
                      </v-chip>
                    </div>
                    <div class="text-caption text-medium-emphasis mt-1">
                      {{ t(`healthScore.about.categoryDescriptions.${cat.name}`) }}
                    </div>
                  </div>
                </v-card-text>
              </v-card>
            </v-col>
          </v-row>
        </section>

        <v-divider class="my-4" />

        <!-- Penalty breakpoints -->
        <section class="about-section">
          <h3 class="text-h6 mb-2 d-flex align-center ga-2">
            <v-icon size="small">mdi-chart-bell-curve-cumulative</v-icon>
            {{ t('healthScore.about.penaltyTitle') }}
          </h3>
          <p class="text-body-2 text-medium-emphasis mb-3">
            {{ t('healthScore.about.penaltyDescription') }}
          </p>

          <v-table density="compact" class="rounded">
            <thead>
              <tr>
                <th>{{ t('healthScore.about.thCategory') }}</th>
                <th>{{ t('healthScore.about.thMetric') }}</th>
                <th>{{ t('healthScore.about.thBreakpoints') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in PENALTY_ROWS" :key="i">
                <td>
                  <v-chip size="x-small" variant="tonal" :prepend-icon="categoryIcon(row.cat)">
                    {{ categoryLabel(row.cat) }}
                  </v-chip>
                </td>
                <td><code>{{ row.metric }}</code></td>
                <td>{{ penaltyPoints(row.pointsKey) }}</td>
              </tr>
            </tbody>
          </v-table>

          <p class="text-caption text-medium-emphasis mt-2">
            {{ t('healthScore.about.xidCalibration') }}
          </p>
        </section>

        <v-divider class="my-4" />

        <!-- Rules and severity -->
        <section class="about-section">
          <h3 class="text-h6 mb-2 d-flex align-center ga-2">
            <v-icon size="small">mdi-flag-checkered</v-icon>
            {{ t('healthScore.about.rulesTitle') }}
          </h3>
          <p class="text-body-2 text-medium-emphasis mb-3">
            {{ t('healthScore.about.rulesDescription') }}
          </p>

          <div class="d-flex ga-2 mb-3 flex-wrap">
            <v-chip size="small" color="error" variant="tonal" prepend-icon="mdi-alert-octagon">
              HIGH
            </v-chip>
            <v-chip size="small" color="warning" variant="tonal" prepend-icon="mdi-alert">
              MEDIUM
            </v-chip>
            <v-chip size="small" color="info" variant="tonal" prepend-icon="mdi-information">
              LOW
            </v-chip>
          </div>

          <v-card
            v-for="(rules, catName) in RULES_BY_CATEGORY"
            :key="catName"
            variant="outlined"
            class="mb-2"
          >
            <v-card-title class="d-flex align-center ga-2 py-2 text-subtitle-2">
              <v-icon size="small">{{ categoryIcon(catName) }}</v-icon>
              {{ categoryLabel(catName) }}
            </v-card-title>
            <v-divider />
            <v-list density="compact">
              <v-list-item
                v-for="rule in rules"
                :key="rule.id"
                :title="ruleTitle(rule.id)"
              >
                <template #subtitle>
                  <div v-if="ruleNote(rule.id)" class="text-caption text-medium-emphasis">
                    {{ ruleNote(rule.id) }}
                  </div>
                </template>
                <template #append>
                  <v-chip size="x-small" variant="tonal">{{ ruleThresholds(rule.id) }}</v-chip>
                </template>
              </v-list-item>
            </v-list>
          </v-card>
        </section>

        <v-divider class="my-4" />

        <!-- Drill down (детализация) -->
        <section class="about-section">
          <h3 class="text-h6 mb-2 d-flex align-center ga-2">
            <v-icon size="small">mdi-arrow-decision</v-icon>
            {{ t('healthScore.about.drilldownTitle') }}
          </h3>
          <p class="text-body-2">{{ t('healthScore.about.drilldown') }}</p>
        </section>

        <v-divider class="my-4" />

        <!-- Trend & seasonal baseline (metrics-backed) -->
        <section class="about-section">
          <h3 class="text-h6 mb-2 d-flex align-center ga-2">
            <v-icon size="small">mdi-chart-line</v-icon>
            {{ t('healthScore.about.trendTitle') }}
          </h3>
          <p class="text-body-2 mb-3">{{ t('healthScore.about.trendIntro') }}</p>

          <ul class="text-body-2 ms-4 mb-3">
            <li>{{ t('healthScore.about.trendBucketing') }}</li>
            <li>{{ t('healthScore.about.trendMedian') }}</li>
          </ul>

          <p class="text-body-2 mb-2">{{ t('healthScore.about.trendUsage') }}</p>
          <ul class="text-body-2 ms-4 mb-3">
            <li>{{ t('healthScore.about.trendDips') }}</li>
            <li>{{ t('healthScore.about.trendLatency') }}</li>
          </ul>

          <v-alert
            type="info"
            variant="tonal"
            density="compact"
            border="start"
            icon="mdi-lightbulb-on-outline"
          >
            {{ t('healthScore.about.trendExample') }}
          </v-alert>

          <p class="text-caption text-medium-emphasis mt-3">
            {{ t('healthScore.about.trendDegrade') }}
          </p>
        </section>
      </v-expansion-panel-text>
    </v-expansion-panel>
  </v-expansion-panels>
</template>

<style scoped>
.formula-code {
  font-family: 'Roboto Mono', ui-monospace, monospace;
  font-size: 0.95rem;
  background-color: rgba(var(--v-theme-on-surface), 0.04);
  padding: 8px 12px;
  border-radius: 4px;
  white-space: pre-wrap;
  margin: 0;
}

.about-section :deep(code) {
  font-family: 'Roboto Mono', ui-monospace, monospace;
  font-size: 0.85rem;
  background-color: rgba(var(--v-theme-on-surface), 0.06);
  padding: 1px 6px;
  border-radius: 3px;
}
</style>
