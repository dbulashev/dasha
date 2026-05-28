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

// Penalty breakpoints — language-independent, hard-coded.
const PENALTY_ROWS = [
  { cat: 'connections', metric: 'total / max_connections', points: '0.60 → 0.80 → 0.95+' },
  { cat: 'connections', metric: 'idle_in_transaction', points: '+5 / шт., max 30' },
  { cat: 'connections', metric: 'longest_transaction_seconds', points: '>300 с, max 20' },
  { cat: 'performance', metric: 'cache_hit_ratio (%)', points: '≥95 → ≥90 → ≥85 → ниже' },
  { cat: 'storage', metric: 'max_dead_ratio (%)', points: '≤20 → 20–30 → >30' },
  { cat: 'storage', metric: 'avg_dead_ratio (%)', points: '>15 → +до 30' },
  { cat: 'storage', metric: 'tables_high_bloat', points: '>5 → +до 30' },
  { cat: 'replication', metric: 'max_replay_lag_seconds', points: '>10 с → max' },
  { cat: 'replication', metric: 'max_lag_bytes', points: '>16 МиБ → max' },
  { cat: 'replication', metric: 'disconnected_replicas', points: '+25 / реплика' },
  { cat: 'maintenance', metric: 'max_xid_age (xid)', points: '500 M → 1 B → 1.5 B' },
  { cat: 'maintenance', metric: 'max_vacuum_age_hours', points: '>168 → >504 → >1440 ч' },
  { cat: 'maintenance', metric: 'tables_never_vacuumed', points: '+5 / шт., max 20' },
  { cat: 'horizon', metric: 'horizon_lag_xids', points: '1 M → 10 M → 100 M' },
  { cat: 'wal_checkpoint', metric: 'requested / total_checkpoints', points: '≥5 % → ≥10 % → ≥20 %' },
  { cat: 'locks', metric: 'penaltyLocks (агрегат)', points: 'накопительный' },
] as const

// Rule thresholds (language-independent). Order within a category matches Registry.
const RULES_BY_CATEGORY: Record<string, { id: string; thresholds: string }[]> = {
  connections: [
    { id: 'high_connection_ratio', thresholds: '≥0.70 / ≥0.85 / ≥0.95' },
    { id: 'idle_in_transaction', thresholds: '≥2 / ≥5 / ≥10' },
    { id: 'long_running_transaction', thresholds: '≥300 / ≥600 / ≥1800 с' },
  ],
  performance: [
    { id: 'low_cache_hit_ratio', thresholds: '<95 / <90 / <85 %' },
    { id: 'track_io_timing_disabled', thresholds: 'LOW' },
  ],
  storage: [
    { id: 'high_max_dead_ratio', thresholds: '≥10 / ≥20 / ≥30 %' },
    { id: 'high_avg_dead_ratio', thresholds: '≥5 / ≥15 / ≥25 %' },
    { id: 'many_bloated_tables', thresholds: '≥5 / ≥10 / ≥20' },
    { id: 'low_hot_update_ratio', thresholds: '<0.80 / <0.65 / <0.50' },
    { id: 'high_newpage_update_ratio', thresholds: '≥0.05 / ≥0.15 / ≥0.25 (PG 16+)' },
  ],
  replication: [
    { id: 'replication_lag_time', thresholds: '≥10 / ≥60 / ≥300 с' },
    { id: 'replication_lag_bytes', thresholds: '≥16 МиБ / ≥256 МиБ / ≥1 ГиБ' },
    { id: 'disconnected_replicas', thresholds: '≥1 / ≥2 / ≥3' },
  ],
  maintenance: [
    { id: 'xid_wraparound_risk', thresholds: '≥150 M / ≥200 M / ≥1.6 B' },
    { id: 'stale_vacuum', thresholds: '≥7 / ≥21 / ≥60 дней' },
    { id: 'tables_never_vacuumed', thresholds: '≥1 / ≥2 / ≥5' },
    { id: 'autovacuum_disabled', thresholds: 'HIGH' },
    { id: 'track_counts_disabled', thresholds: 'HIGH' },
    { id: 'tables_with_autovacuum_off', thresholds: '≥1 / ≥5 / ≥20' },
    { id: 'relfrozenxid_age_outlier', thresholds: '≥200 M / ≥500 M / ≥1 B' },
    { id: 'stale_planner_stats', thresholds: '≥3 / ≥10 / ≥30' },
    { id: 'analyze_disabled_tables', thresholds: '≥1 / ≥5 / ≥20' },
  ],
  horizon: [{ id: 'horizon_lag_xids', thresholds: '≥1 M / ≥10 M / ≥100 M' }],
  wal_checkpoint: [
    { id: 'requested_checkpoint_ratio', thresholds: '≥5 / ≥10 / ≥20 %' },
    { id: 'wal_level_minimal_with_replicas', thresholds: 'HIGH' },
    { id: 'wal_level_logical_without_publications', thresholds: 'LOW' },
  ],
  locks: [
    { id: 'active_lock_waiters', thresholds: '≥1 / ≥3 / ≥10' },
    { id: 'longest_lock_wait_seconds', thresholds: '≥10 / ≥30 / ≥60 с' },
    { id: 'ungranted_locks', thresholds: '≥2 / ≥5 / ≥15' },
    { id: 'deadlocks_rate', thresholds: 'LOW при >0' },
    { id: 'lock_pool_saturation', thresholds: '≥0.4 / ≥0.6 / ≥0.8' },
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
                <td>{{ row.points }}</td>
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
                  <v-chip size="x-small" variant="tonal">{{ rule.thresholds }}</v-chip>
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
