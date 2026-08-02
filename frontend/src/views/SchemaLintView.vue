<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getSchemaLint } from '@/api/gen/default/default'
import type { SchemaLintFinding, SchemaLintReport, SchemaLintSkip } from '@/api/models/index'
import { GetSchemaLintLevel } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useDebouncedRef } from '@/composables/useDebouncedRef'
import { useViewError } from '@/composables/useViewError'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { usePrefsStore } from '@/stores/prefs'
import PaginationControls from '@/components/PaginationControls.vue'
import SchemaLintSummaryBar from '@/components/schema-lint/SchemaLintSummaryBar.vue'
import SchemaLintFindingsTable from '@/components/schema-lint/SchemaLintFindingsTable.vue'
import SchemaLintSkippedAlert from '@/components/schema-lint/SchemaLintSkippedAlert.vue'
import SchemaLintDatabasesCard from '@/components/schema-lint/SchemaLintDatabasesCard.vue'

const { t, tm } = useI18n()
const { clusterName, hostName, databaseName } = useClusterInfo()
const { onError } = useViewError()
const prefs = usePrefsStore()

// Nullable because `clearable` writes null, not '', when a field is cleared.
const levelFilter = ref<GetSchemaLintLevel | null>(null)
const codeFilter = ref<string[]>([])
const schemaFilter = ref<string | null>('')
const objectFilter = ref<string | null>('')
const groupBy = ref<'code' | 'object'>('code')

const schemaQuery = useDebouncedRef(schemaFilter, 400)
const objectQuery = useDebouncedRef(objectFilter, 400)

const findings = ref<SchemaLintFinding[]>([])
const skipped = ref<SchemaLintSkip[]>([])
const summary = ref({ error: 0, warning: 0, notice: 0 })
const total = ref(0)
const truncated = ref(false)
const durationMs = ref(0)
const loading = ref(false)
const page = ref(1)

// Guards against out-of-order responses: changing a filter or the database can
// leave an older request in flight whose late reply would clobber the newer one.
let reqId = 0

async function load(p = 1) {
  if (!clusterName.value || !hostName.value || !databaseName.value) return

  const myId = ++reqId
  loading.value = true

  try {
    const pageSize = prefs.pageSize
    const res = await getSchemaLint({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      level: levelFilter.value ?? undefined,
      code: codeFilter.value.length ? codeFilter.value : undefined,
      schema: schemaQuery.value?.trim() || undefined,
      object: objectQuery.value?.trim() || undefined,
      limit: pageSize,
      offset: (p - 1) * pageSize,
    })
    const body = assertOk<SchemaLintReport>(res)
    if (myId !== reqId) return // superseded — leave state to the newer load

    findings.value = body?.findings ?? []
    skipped.value = body?.skipped ?? []
    summary.value = body?.summary ?? { error: 0, warning: 0, notice: 0 }
    total.value = body?.total ?? 0
    truncated.value = body?.truncated ?? false
    durationMs.value = body?.duration_ms ?? 0
    page.value = p
  } catch (err) {
    if (myId !== reqId) return
    onError(getErrorMessage(err), err)
    findings.value = []
    skipped.value = []
    summary.value = { error: 0, warning: 0, notice: 0 }
    total.value = 0
  } finally {
    if (myId === reqId) loading.value = false
  }
}

watch(
  [clusterName, hostName, databaseName, levelFilter, codeFilter, schemaQuery, objectQuery, () => prefs.pageSize],
  () => load(),
  { immediate: true },
)

const hasMore = computed(() => page.value * prefs.pageSize < total.value)

// The check list comes from the i18n bundle, which has an entry per code, and
// not from the findings on screen: those are one page of a filtered result, so
// deriving the options from them would let the level filter — or simply paging —
// take codes out of the very list the filter is chosen from.
const codeOptions = computed(() => {
  const checks = tm('schemaLint.checks')
  if (!checks || typeof checks !== 'object') return []

  return Object.keys(checks)
    .map((code) => ({ value: code, title: t(`schemaLint.checks.${code}.title`) }))
    .sort((a, b) => a.title.localeCompare(b.title))
})

const groupOptions = computed(() => [
  { value: 'code', title: t('schemaLint.page.groupByCheck') },
  { value: 'object', title: t('schemaLint.page.groupByObject') },
])

// Clicking a counter filters by that level, clicking it again clears it.
function onLevelPick(level: GetSchemaLintLevel | null) {
  levelFilter.value = levelFilter.value === level ? null : level
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-2 flex-wrap">
      <v-icon start icon="mdi-clipboard-check-outline" /><span>{{ t('schemaLint.page.title') }}</span>
      <v-tooltip :text="t('schemaLint.page.hint')" location="bottom" max-width="480">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>

    <v-card-text>
      <SchemaLintSummaryBar
        :summary="summary"
        :active-level="levelFilter"
        :duration-ms="durationMs"
        @pick="onLevelPick"
      />

      <v-alert
        v-if="truncated"
        type="warning"
        variant="tonal"
        density="compact"
        class="mb-3"
        icon="mdi-scissors-cutting"
      >
        {{ t('schemaLint.page.truncated') }}
      </v-alert>

      <SchemaLintSkippedAlert :skipped="skipped" />

      <SchemaLintDatabasesCard />

      <div class="d-flex align-center ga-2 flex-wrap mb-3">
        <v-select
          v-model="codeFilter"
          :items="codeOptions"
          :label="t('schemaLint.page.checkFilter')"
          density="compact"
          variant="outlined"
          multiple
          chips
          closable-chips
          hide-details
          style="min-width: 260px; max-width: 420px"
        />
        <v-text-field
          v-model="schemaFilter"
          :label="t('header.schema')"
          prepend-inner-icon="mdi-magnify"
          density="compact"
          variant="outlined"
          clearable
          hide-details
          style="max-width: 200px"
        />
        <v-text-field
          v-model="objectFilter"
          :label="t('schemaLint.page.objectFilter')"
          prepend-inner-icon="mdi-magnify"
          density="compact"
          variant="outlined"
          clearable
          hide-details
          style="max-width: 200px"
        />
        <v-spacer />
        <v-btn-toggle v-model="groupBy" density="compact" variant="outlined" mandatory>
          <v-btn v-for="opt in groupOptions" :key="opt.value" :value="opt.value" size="small">
            {{ opt.title }}
          </v-btn>
        </v-btn-toggle>
      </div>

      <SchemaLintFindingsTable :findings="findings" :loading="loading" :group-by="groupBy" />
      <PaginationControls :page="page" :has-more="hasMore" @update:page="load" />
    </v-card-text>
  </v-card>
</template>
