<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { LogPlanRegression } from '@/api/models'
import { fmtCompact, fmtCompactFloat, fmtMs } from '@/utils/format'
import { copyToClipboard, highlightSql, truncateSql, SQL_PREVIEW_MAX } from '@/utils/sql'
import { severityColor } from '@/components/explain/planText'
import '@/assets/sql-highlight.css'

defineProps<{
  item: LogPlanRegression
}>()

const emit = defineEmits<{
  showSql: [item: LogPlanRegression]
}>()

const { t } = useI18n()

function ratioText(r: number): string {
  return r > 0 ? `${fmtCompactFloat(r)}×` : '—'
}
</script>

<template>
  <v-card variant="outlined" class="mb-3">
    <v-card-title class="text-body-1 pb-1 d-flex align-center flex-wrap ga-2">
      <v-chip :color="severityColor(item.severity)" size="small" variant="flat" label>
        {{ item.severity }}
      </v-chip>
      <template v-if="item.query_id">
        <span class="text-body-2">queryid: <span class="text-mono">{{ item.query_id }}</span></span>
        <v-btn
          icon="mdi-content-copy"
          variant="text"
          size="x-small"
          :title="t('logs.insights.plans.copyQueryId')"
          @click="copyToClipboard(item.query_id ?? '')"
        />
      </template>
      <v-chip
        v-for="reason in item.reasons"
        :key="reason"
        size="small"
        variant="tonal"
        label
      >
        {{ t(`logs.insights.compare.reason.${reason}`) }}
      </v-chip>
    </v-card-title>

    <v-card-text class="pt-0">
      <v-row dense>
        <v-col cols="6" md="3">
          <div class="text-caption text-medium-emphasis">p50</div>
          <div class="text-body-2">
            {{ fmtMs(item.baseline.p50_ms, t) }} → {{ fmtMs(item.current.p50_ms, t) }}
            <span class="text-caption text-medium-emphasis ms-1">{{ ratioText(item.p50_ratio) }}</span>
          </div>
        </v-col>
        <v-col cols="6" md="3">
          <div class="text-caption text-medium-emphasis">p95</div>
          <div class="text-body-2">
            {{ fmtMs(item.baseline.p95_ms, t) }} → {{ fmtMs(item.current.p95_ms, t) }}
            <span class="text-caption text-medium-emphasis ms-1">{{ ratioText(item.p95_ratio) }}</span>
          </div>
        </v-col>
        <v-col cols="6" md="3">
          <div class="text-caption text-medium-emphasis">{{ t('logs.insights.compare.count') }}</div>
          <div class="text-body-2">
            {{ fmtCompact(item.baseline_count) }} → {{ fmtCompact(item.current_count) }}
          </div>
        </v-col>
        <v-col v-if="item.lost_indexes.length || item.added_indexes.length" cols="6" md="3">
          <div class="text-caption text-medium-emphasis">{{ t('logs.insights.compare.indexes') }}</div>
          <div v-if="item.lost_indexes.length" class="text-body-2 text-mono text-error">
            −{{ item.lost_indexes.join(', ') }}
          </div>
          <div v-if="item.added_indexes.length" class="text-body-2 text-mono">
            +{{ item.added_indexes.join(', ') }}
          </div>
        </v-col>
      </v-row>

      <div class="d-flex align-start mt-2">
        <code
          class="sql-highlight text-mono text-body-2 text-medium-emphasis flex-grow-1"
          v-html="highlightSql(truncateSql(item.query_text))"
        ></code>
        <v-btn
          icon="mdi-content-copy"
          variant="text"
          size="x-small"
          class="ml-1 flex-shrink-0"
          @click="copyToClipboard(item.query_text)"
        />
        <v-btn
          v-if="item.query_text.length > SQL_PREVIEW_MAX"
          size="small"
          variant="text"
          class="ml-1 flex-shrink-0"
          @click="emit('showSql', item)"
        >
          {{ t('report.showSql') }}
        </v-btn>
      </div>
    </v-card-text>
  </v-card>
</template>
