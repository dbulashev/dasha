<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { severityColor } from '@/components/explain/planText'

const { t } = useI18n()

// Thresholds are the ones the rules trip on; the identifiers are the plan's own,
// so the table reads the same in every language.
const RULES = [
  { code: 'seq_scan_large', severity: 'MEDIUM', needs: '', when: 'Seq Scan + Filter, table_rows ≥ 100k (cost ≥ 1k)' },
  { code: 'cost_hotspot', severity: 'LOW', needs: '', when: 'self_cost ≥ 50% total_cost, ≥ 100' },
  { code: 'nested_loop_blowup', severity: 'MEDIUM', needs: '', when: 'outer ≥ 1k, outer × inner ≥ 1M' },
  { code: 'index_candidate_join', severity: 'HIGH', needs: '', when: 'Nested Loop → Seq Scan, cond ≠ ∅' },
  { code: 'sort_estimate_spill', severity: 'MEDIUM', needs: 'work_mem', when: 'plan_rows × width > work_mem' },
  { code: 'cte_materialize', severity: 'LOW', needs: '', when: 'CTE Scan | Materialize, plan_rows ≥ 100k' },
  { code: 'row_misestimate', severity: 'HIGH', needs: 'actual', when: 'plan/actual ≥ 10×, rows ≥ 100' },
  { code: 'sort_spill_actual', severity: 'MEDIUM', needs: 'actual', when: 'sort_method ⊃ external' },
  { code: 'filter_discards_rows', severity: 'MEDIUM', needs: 'actual', when: 'removed ≥ 90%, rows ≥ 10k' },
  { code: 'heap_fetches_high', severity: 'MEDIUM', needs: 'actual', when: 'Index Only Scan, fetches ≥ 20% rows, ≥ 1k' },
  { code: 'loops_blowup', severity: 'HIGH', needs: 'actual', when: 'loops ≥ 10× expected, ≥ 1k' },
  { code: 'bitmap_lossy', severity: 'LOW', needs: 'actual', when: 'lossy ≥ 10% blocks' },
  { code: 'workers_not_launched', severity: 'LOW', needs: 'actual', when: 'launched < planned' },
  { code: 'jit_overhead', severity: 'LOW', needs: 'timing', when: 'jit ≥ 25% total, ≥ 10 ms' },
  { code: 'trigger_time', severity: 'LOW', needs: 'timing', when: 'triggers ≥ 25% total, ≥ 10 ms' },
] as const

const REASONS = [
  { code: 'new_shape', when: 'hash ∉ baseline', severity: 'MEDIUM' },
  { code: 'lost_index', when: 'index ∈ baseline, ∉ current', severity: 'HIGH' },
  { code: 'slower', when: 'p95 ≥ 2×, ≥ 20 plans each side (HIGH: ≥ 10×)', severity: 'MEDIUM' },
] as const

const NUMBERS = ['count', 'percentiles', 'sumMax', 'rows', 'buffers', 'cost'] as const
</script>

<template>
  <v-expansion-panels variant="accordion" class="mb-4">
    <v-expansion-panel>
      <v-expansion-panel-title color="surface">
        <template #default="{ expanded }">
          <span class="d-flex align-center ga-2">
            <v-icon size="small">mdi-{{ expanded ? 'book-open-variant' : 'book-outline' }}</v-icon>
            <span class="text-subtitle-1 font-weight-medium">{{ t('logs.insights.about.toggle') }}</span>
          </span>
        </template>
      </v-expansion-panel-title>

      <v-expansion-panel-text>
        <section class="mb-4">
          <h3 class="text-subtitle-1 font-weight-medium mb-2">{{ t('logs.insights.about.sourceTitle') }}</h3>
          <ul class="text-body-2 ms-4">
            <li>{{ t('logs.insights.about.sourceRecords') }}</li>
            <li>{{ t('logs.insights.about.sourceThreshold') }}</li>
            <li>{{ t('logs.insights.about.sourceScan') }}</li>
            <li>{{ t('logs.insights.about.sourceSnapshot') }}</li>
            <li>{{ t('logs.insights.about.sourceMasking') }}</li>
          </ul>
        </section>

        <v-divider class="my-3" />

        <section class="mb-4">
          <h3 class="text-subtitle-1 font-weight-medium mb-2">{{ t('logs.insights.about.groupsTitle') }}</h3>
          <ul class="text-body-2 ms-4 mb-3">
            <li>{{ t('logs.insights.about.groupKey') }}</li>
            <li>{{ t('logs.insights.about.groupSample') }}</li>
            <li>{{ t('logs.insights.about.groupTop') }}</li>
          </ul>

          <v-table density="compact" class="rounded">
            <thead>
              <tr>
                <th>{{ t('logs.insights.about.thNumber') }}</th>
                <th>{{ t('logs.insights.about.thHow') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="n in NUMBERS" :key="n">
                <td class="text-mono">{{ t(`logs.insights.about.numbers.${n}.name`) }}</td>
                <td class="text-body-2 text-medium-emphasis">{{ t(`logs.insights.about.numbers.${n}.how`) }}</td>
              </tr>
            </tbody>
          </v-table>
        </section>

        <v-divider class="my-3" />

        <section class="mb-4">
          <h3 class="text-subtitle-1 font-weight-medium mb-2">{{ t('logs.insights.about.rulesTitle') }}</h3>
          <v-table density="compact" class="rounded">
            <thead>
              <tr>
                <th>{{ t('logs.insights.about.thRule') }}</th>
                <th>{{ t('logs.insights.about.thSeverity') }}</th>
                <th>{{ t('logs.insights.about.thNeeds') }}</th>
                <th>{{ t('logs.insights.about.thWhen') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in RULES" :key="r.code">
                <td class="text-mono">{{ r.code }}</td>
                <td>
                  <v-chip :color="severityColor(r.severity)" size="x-small" variant="flat" label>
                    {{ r.severity }}
                  </v-chip>
                </td>
                <td class="text-mono text-medium-emphasis">{{ r.needs || '—' }}</td>
                <td class="text-mono text-medium-emphasis">{{ r.when }}</td>
              </tr>
            </tbody>
          </v-table>
          <div class="text-caption text-medium-emphasis mt-2">{{ t('logs.insights.about.rulesDormant') }}</div>
        </section>

        <v-divider class="my-3" />

        <section class="mb-4">
          <h3 class="text-subtitle-1 font-weight-medium mb-2">{{ t('logs.insights.about.compareTitle') }}</h3>
          <ul class="text-body-2 ms-4 mb-3">
            <li>{{ t('logs.insights.about.compareShape') }}</li>
            <li>{{ t('logs.insights.about.compareBoth') }}</li>
            <li>{{ t('logs.insights.about.compareCost') }}</li>
          </ul>

          <v-table density="compact" class="rounded">
            <thead>
              <tr>
                <th>{{ t('logs.insights.about.thReason') }}</th>
                <th>{{ t('logs.insights.about.thSeverity') }}</th>
                <th>{{ t('logs.insights.about.thWhen') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in REASONS" :key="r.code">
                <td class="text-mono">{{ r.code }}</td>
                <td>
                  <v-chip :color="severityColor(r.severity)" size="x-small" variant="flat" label>
                    {{ r.severity }}
                  </v-chip>
                </td>
                <td class="text-mono text-medium-emphasis">{{ r.when }}</td>
              </tr>
            </tbody>
          </v-table>
        </section>

        <v-divider class="my-3" />

        <section>
          <h3 class="text-subtitle-1 font-weight-medium mb-2">{{ t('logs.insights.about.categoriesTitle') }}</h3>
          <ul class="text-body-2 ms-4">
            <li>{{ t('logs.insights.about.categoryOrder') }}</li>
            <li>{{ t('logs.insights.about.categoryTemplates') }}</li>
            <li>{{ t('logs.insights.about.categoryBurst') }}</li>
          </ul>
        </section>
      </v-expansion-panel-text>
    </v-expansion-panel>
  </v-expansion-panels>
</template>
