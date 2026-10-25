<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { analyzePlanLifecycle } from '@/api/gen/default/default'
import type { PlanLifecycleAnalysis, PlanLifecycleMatch, PlanLifecycleParseError } from '@/api/models'
import PlanTree from '@/components/explain/PlanTree.vue'
import { pathKey } from '@/components/explain/planText'
import IndexChains from '@/components/planlife/IndexChains.vue'
import RelationPanel from '@/components/planlife/RelationPanel.vue'
import TraceTimeline from '@/components/planlife/TraceTimeline.vue'
import { matched, pathTypeLabel, pathsById, planTree } from '@/components/planlife/planlife'

const { t } = useI18n()

const traceText = ref('')
const traceName = ref('')
const planText = ref('')
const planName = ref('')
const loading = ref(false)
const error = ref('')
const analysis = ref<PlanLifecycleAnalysis | null>(null)

const tab = ref('plan')
const selectedPath = ref<number | null>(null)
const timelinePath = ref<number | null>(null)

async function readFile(file: File | null | undefined) {
  if (!file) return
  traceText.value = await file.text()
  traceName.value = file.name
}

// a new plan for an analysis already on screen is applied right away
async function readPlan(file: File | null | undefined) {
  if (!file) return
  planText.value = await file.text()
  planName.value = file.name
  if (analysis.value) requestAnalyze()
}

function first(files: File | File[] | null): File | null {
  return Array.isArray(files) ? (files[0] ?? null) : files
}

function onFiles(files: File | File[] | null) {
  void readFile(first(files))
}

function onPlanFiles(files: File | File[] | null) {
  void readPlan(first(files))
}

function onDrop(e: DragEvent) {
  void readFile(e.dataTransfer?.files?.[0])
}

function onPlanDrop(e: DragEvent) {
  void readPlan(e.dataTransfer?.files?.[0])
}

function removePlan() {
  planText.value = ''
  planName.value = ''
  if (analysis.value) requestAnalyze()
}

function requestAnalyze() {
  if (!traceText.value.trim()) return
  void analyze()
}

function parseErrorText(e: PlanLifecycleParseError): string {
  return t('planLifecycle.error.parse', {
    input: t(`planLifecycle.input.${e.input}`),
    code: e.code,
    where: [e.field, e.offset != null ? `offset ${e.offset}` : ''].filter(Boolean).join(', ') || '—',
  })
}

async function analyze() {
  loading.value = true
  error.value = ''

  try {
    const plan = planText.value.trim() ? planText.value : undefined
    const res = await analyzePlanLifecycle({ trace: traceText.value, plan })
    switch (res.status) {
      case 200:
        analysis.value = res.data
        selectedPath.value = res.data.final.path_id ?? null
        timelinePath.value = null
        tab.value = 'plan'
        break
      case 422:
        error.value = parseErrorText(res.data)
        break
      case 413:
        error.value = t('planLifecycle.error.tooLarge')
        break
      case 429:
        error.value = t('planLifecycle.error.rateLimited')
        break
      default:
        error.value = (res.data as { message?: string } | undefined)?.message ?? `HTTP ${res.status}`
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

const tree = computed(() => (analysis.value ? planTree(analysis.value) : null))
const byPlan = computed(() => (analysis.value ? matched(analysis.value) : false))

function matchColor(m: PlanLifecycleMatch): 'success' | 'info' | 'error' | 'warning' {
  switch (m.status) {
    case 'match':
      return 'success'
    case 'match_with_notes':
      return 'info'
    case 'mismatch':
      return 'error'
    default:
      return 'warning'
  }
}

function mismatchText(m: PlanLifecycleMatch): string {
  if (m.query_text === 'differs') return t('planLifecycle.match.queryTextDiffers')
  const d = m.mismatch
  if (!d) return ''
  const p = byId.value.get(d.path_id)
  return t('planLifecycle.match.diff', {
    path: p ? `${pathTypeLabel(p.pathtype)} #${d.path_id}` : `#${d.path_id}`,
    expected: d.expected,
    found: d.found,
    node: d.plan_node.length ? d.plan_node.join('.') : t('planLifecycle.match.root'),
  })
}
const byId = computed(() => (analysis.value ? pathsById(analysis.value) : new Map()))

const selectedRel = computed(() => {
  const p = selectedPath.value != null ? byId.value.get(selectedPath.value) : undefined
  return p?.rel ?? null
})

const highlight = computed(() => {
  if (selectedPath.value == null || !tree.value) return null
  const key = tree.value.keyByPath.get(selectedPath.value)
  return key == null ? null : key === '' ? [] : key.split('.').map(Number)
})

// plan nodes without a path of their own (Hash) select nothing
function onTreeSelect(path: number[]) {
  const id = tree.value?.pathByKey.get(pathKey(path))
  if (id != null) selectedPath.value = id
}

function selectPath(id: number) {
  selectedPath.value = id
  tab.value = 'plan'
}

function alternatives(path: number[]): number {
  const id = tree.value?.pathByKey.get(pathKey(path))
  const p = id != null ? byId.value.get(id) : undefined
  const r = p ? analysis.value?.relations.find(x => x.key === p.rel) : undefined
  if (!r) return 0
  return r.path_ids.length - 1 + r.precheck.reduce((n, g) => n + g.count, 0)
}

// index_not_examined is shown on the index tab, next to the list it is about
const traceCaveats = computed(
  () =>
    analysis.value?.caveats.filter(c => c.scope === 'trace' && c.code !== 'index_not_examined_unobservable') ?? [],
)
</script>

<template>
  <div>
    <div class="text-h5 mb-1">{{ t('planLifecycle.title') }}</div>
    <div class="text-body-2 text-medium-emphasis mb-4">{{ t('planLifecycle.subtitle') }}</div>

    <v-card variant="outlined" class="mb-4" @dragover.prevent @drop.prevent="onDrop">
      <v-card-text>
        <div class="d-flex flex-wrap align-center ga-3">
          <v-file-input
            :label="t('planLifecycle.traceFile')"
            accept=".json,application/json"
            density="compact"
            hide-details
            prepend-icon="mdi-file-upload-outline"
            style="max-width: 420px"
            @update:model-value="onFiles"
          />
          <span class="text-caption text-medium-emphasis">{{ t('planLifecycle.dropHint') }}</span>
          <v-spacer />
          <v-btn color="primary" :loading="loading" :disabled="!traceText.trim()" @click="requestAnalyze">
            {{ t('planLifecycle.analyze') }}
          </v-btn>
        </div>
        <div class="d-flex flex-wrap align-center ga-3 mt-3" @dragover.prevent.stop @drop.prevent.stop="onPlanDrop">
          <v-file-input
            :label="t('planLifecycle.planFile')"
            accept=".json,.txt,application/json,text/plain"
            density="compact"
            hide-details
            prepend-icon="mdi-file-tree-outline"
            style="max-width: 420px"
            @update:model-value="onPlanFiles"
          />
          <span class="text-caption text-medium-emphasis">{{ t('planLifecycle.planHint') }}</span>
          <v-btn v-if="planText.trim()" variant="text" size="small" @click="removePlan">
            {{ t('planLifecycle.removePlan') }}
          </v-btn>
        </div>
        <v-expansion-panels class="mt-3" variant="accordion">
          <v-expansion-panel :title="t('planLifecycle.paste')">
            <v-expansion-panel-text>
              <v-textarea
                v-model="traceText"
                :placeholder="'{ &quot;events&quot;: [...], &quot;paths&quot;: [...], ... }'"
                rows="6"
                class="text-mono"
                hide-details
                @update:model-value="traceName = ''"
              />
            </v-expansion-panel-text>
          </v-expansion-panel>
          <v-expansion-panel :title="t('planLifecycle.pastePlan')">
            <v-expansion-panel-text>
              <v-textarea
                v-model="planText"
                :placeholder="t('planLifecycle.planPlaceholder')"
                rows="6"
                class="text-mono"
                hide-details
                @update:model-value="planName = ''"
              />
            </v-expansion-panel-text>
          </v-expansion-panel>
        </v-expansion-panels>
        <div v-if="traceName || planName" class="text-caption mt-2">
          {{ [traceName, planName].filter(Boolean).join(' + ') }}
        </div>
      </v-card-text>
    </v-card>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4" closable @click:close="error = ''">
      {{ error }}
    </v-alert>

    <template v-if="analysis">
      <v-alert
        v-for="(c, i) in traceCaveats"
        :key="i"
        :type="c.code === 'events_truncated' || c.code === 'final_none' ? 'warning' : 'info'"
        variant="tonal"
        density="compact"
        class="mb-2"
      >
        {{ t(`planLifecycle.caveat.${c.code}`, c.params ?? {}) }}
      </v-alert>

      <v-alert
        v-if="analysis.match"
        :type="matchColor(analysis.match)"
        variant="tonal"
        density="compact"
        class="mb-2"
      >
        <div class="font-weight-medium">{{ t(`planLifecycle.match.status.${analysis.match.status}`) }}</div>
        <div v-if="analysis.match.status === 'mismatch'">{{ mismatchText(analysis.match) }}</div>
        <div v-for="(n, i) in analysis.match.notes" :key="`n${i}`" class="text-caption">
          #{{ n.path_id }}: {{ t(`planLifecycle.match.note.${n.code}`) }}
        </div>
        <div v-for="(u, i) in analysis.match.unmatched" :key="`u${i}`" class="text-caption">
          {{ u.plan_node.join('.') || t('planLifecycle.match.root') }}: {{ t(`planLifecycle.match.unmatched.${u.reason}`) }}
        </div>
        <div v-if="analysis.final.status === 'by_plan'" class="text-caption">{{ t('planLifecycle.match.finalByPlan') }}</div>
      </v-alert>

      <div v-if="analysis.query" class="text-caption text-mono text-medium-emphasis my-3 planlife-query">
        {{ analysis.query }}
      </div>

      <v-tabs v-model="tab" class="mb-3">
        <v-tab value="plan">{{ t('planLifecycle.tab.plan') }}</v-tab>
        <v-tab value="indexes">{{ t('planLifecycle.tab.indexes') }} ({{ analysis.indexes.length }})</v-tab>
        <v-tab value="timeline">{{ t('planLifecycle.tab.timeline') }} ({{ analysis.timeline.length }})</v-tab>
      </v-tabs>

      <v-window v-model="tab">
        <v-window-item value="plan">
          <v-card v-if="tree?.root" variant="outlined" class="mb-4">
            <v-card-title class="text-subtitle-1">
              {{ t(byPlan ? 'planLifecycle.planTree' : 'planLifecycle.finalTree') }}
            </v-card-title>
            <v-card-text>
              <PlanTree
                :root="tree.root"
                :findings="byPlan ? analysis.plan?.findings : undefined"
                :highlight="highlight"
                selectable
                @select="onTreeSelect"
              >
                <template #extra="{ path }">
                  <v-chip v-if="alternatives(path)" size="x-small" variant="tonal" label>
                    {{ t('planLifecycle.alternatives', { n: alternatives(path) }) }}
                  </v-chip>
                </template>
              </PlanTree>
            </v-card-text>
          </v-card>
          <v-alert v-else type="warning" variant="tonal" class="mb-4">{{ t('planLifecycle.noFinal') }}</v-alert>

          <v-card v-if="selectedRel" variant="outlined">
            <v-card-text>
              <RelationPanel
                :analysis="analysis"
                :rel="selectedRel"
                :selected="selectedPath"
                @select="id => (selectedPath = id)"
              />
            </v-card-text>
          </v-card>
        </v-window-item>

        <v-window-item value="indexes">
          <IndexChains :analysis="analysis" @select="selectPath" />
        </v-window-item>

        <v-window-item value="timeline">
          <TraceTimeline v-model:path-filter="timelinePath" :analysis="analysis" @select="selectPath" />
        </v-window-item>
      </v-window>
    </template>
  </div>
</template>

<style scoped>
.planlife-query {
  white-space: pre-wrap;
}
</style>
