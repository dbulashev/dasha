<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import type { HealthScoreRecommendation } from '@/api/models/index'

const props = defineProps<{
  rec: HealthScoreRecommendation
}>()

const { t, te } = useI18n()
const route = useRoute()
const expanded = ref(false)

// Backend returns plain paths like "/queries" or "/queries?tab=stats".
// Wrap them with the current cluster/host/db context so the link lands on
// the right view, matching the pattern used by App.vue#withQuery.
const relatedLink = computed(() => {
  const raw = props.rec.related_route
  if (!raw) return null

  const [path, search] = raw.split('?')
  const cluster = (route.params.clustername as string) ?? ''
  const host = route.query.host ? String(route.query.host) : null
  const db = route.query.db ? String(route.query.db) : null

  const query: Record<string, string> = {}
  if (search) {
    for (const [k, v] of new URLSearchParams(search)) query[k] = v
  }
  if (host) query.host = host
  if (db) query.db = db

  return {
    path: `${path}/${cluster}`,
    query,
  }
})

const i18nBase = computed(() => `healthScore.recommendations.${props.rec.rule_id}`)

const i18nContext = computed<Record<string, unknown>>(() => {
  const raw = props.rec.metric_value
  // metric_pct: same value rendered as a percentage with one decimal,
  // for rules whose metric_value is a 0..1 ratio (HOT, newpage, requested_checkpoints,
  // lock_pool_saturation). Locale strings choose which form to use.
  return {
    metric_value: raw,
    metric_pct: Number.isFinite(raw) ? (raw * 100).toFixed(1) : raw,
    ...(props.rec.context ?? {}),
  }
})

const title = computed(() => t(`${i18nBase.value}.title`, i18nContext.value))
const short = computed(() => t(`${i18nBase.value}.short`, i18nContext.value))
const hasDetail = computed(() => te(`${i18nBase.value}.detail`))
const detail = computed(() =>
  hasDetail.value ? t(`${i18nBase.value}.detail`, i18nContext.value) : '',
)
const hasSql = computed(() => te(`${i18nBase.value}.sql`))
const sql = computed(() => (hasSql.value ? t(`${i18nBase.value}.sql`, i18nContext.value) : ''))

const severityColor = computed(() => {
  switch (props.rec.severity) {
    case 'HIGH':
      return 'error'
    case 'MEDIUM':
      return 'warning'
    case 'LOW':
      return 'info'
    default:
      return 'default'
  }
})

const severityIcon = computed(() => {
  switch (props.rec.severity) {
    case 'HIGH':
      return 'mdi-alert-octagon'
    case 'MEDIUM':
      return 'mdi-alert'
    case 'LOW':
      return 'mdi-information-outline'
    default:
      return 'mdi-circle-outline'
  }
})

const copied = ref(false)

async function copySql() {
  try {
    await navigator.clipboard.writeText(sql.value)
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 1500)
  } catch {
    // Clipboard API unavailable; ignore.
  }
}
</script>

<template>
  <v-card variant="outlined" class="mb-2">
    <v-card-text class="pa-3">
      <div class="d-flex align-center ga-2">
        <v-chip :color="severityColor" variant="flat" size="small" :prepend-icon="severityIcon">
          {{ rec.severity }}
        </v-chip>
        <span class="text-body-1 font-weight-medium">{{ title }}</span>
        <v-spacer />
        <v-btn
          v-if="relatedLink"
          variant="text"
          size="small"
          :to="relatedLink"
          append-icon="mdi-arrow-right"
        >
          {{ t('healthScore.recommendations.openRelated') }}
        </v-btn>
      </div>

      <div class="text-body-2 text-medium-emphasis mt-2">{{ short }}</div>

      <v-expand-transition>
        <div v-if="expanded" class="mt-3">
          <div v-if="detail" class="text-body-2 mb-2" style="white-space: pre-line">
            {{ detail }}
          </div>
          <div v-if="sql" class="d-flex align-start ga-2">
            <pre class="rec-sql flex-grow-1"><code>{{ sql }}</code></pre>
            <v-btn
              size="small"
              variant="tonal"
              :prepend-icon="copied ? 'mdi-check' : 'mdi-content-copy'"
              @click="copySql"
            >
              {{ copied ? t('healthScore.recommendations.copied') : t('healthScore.recommendations.copy') }}
            </v-btn>
          </div>
        </div>
      </v-expand-transition>

      <div v-if="hasDetail || hasSql" class="mt-2">
        <v-btn
          variant="text"
          size="small"
          :append-icon="expanded ? 'mdi-chevron-up' : 'mdi-chevron-down'"
          @click="expanded = !expanded"
        >
          {{ expanded
            ? t('healthScore.recommendations.hideHowTo')
            : t('healthScore.recommendations.showHowTo') }}
        </v-btn>
      </div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.rec-sql {
  background: rgba(0, 0, 0, 0.04);
  padding: 8px 12px;
  border-radius: 4px;
  font-size: 0.85em;
  overflow-x: auto;
  margin: 0;
}
</style>
