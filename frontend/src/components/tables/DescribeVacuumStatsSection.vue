<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getTablesDescribeVacuumStats } from '@/api/gen/default/default'
import type { VacuumStats } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

const props = defineProps<{ schema: string; table: string }>()

const { t } = useI18n()
const { clusterName, hostName, databaseName } = useClusterInfo()

const data = ref<VacuumStats | null>(null)
const loading = ref(false)

async function load() {
  if (!clusterName.value || !hostName.value || !databaseName.value || !props.schema || !props.table) {
    data.value = null
    return
  }
  loading.value = true
  try {
    const res = await getTablesDescribeVacuumStats({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      schema: props.schema,
      table: props.table,
    })
    data.value = assertOk<VacuumStats>(res)
  } catch (err) {
    console.error(getErrorMessage(err))
    data.value = null
  } finally {
    loading.value = false
  }
}

watch([clusterName, hostName, databaseName, () => props.schema, () => props.table], () => load(), { immediate: true })

function fmtAgo(ts: string | null | undefined): string {
  if (!ts) return t('describe.never')
  const diff = Date.now() - new Date(ts).getTime()
  const sec = Math.floor(diff / 1000)
  if (sec < 60) return `${sec}s ago`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m ago`
  const hrs = Math.floor(min / 60)
  if (hrs < 24) return `${hrs}h ago`
  const days = Math.floor(hrs / 24)
  return `${days}d ago`
}

function progressPct(current: number, threshold: number): number {
  if (threshold <= 0) return 0
  return Math.min(100, Math.round(current / threshold * 100))
}

function progressColor(pct: number): string {
  if (pct >= 80) return 'error'
  if (pct >= 50) return 'warning'
  return 'success'
}

function fmtNum(n: number): string {
  return n.toLocaleString()
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-title>{{ t('describe.vacuumStats') }}</v-card-title>
    <v-card-text v-if="loading">
      <v-progress-linear indeterminate />
    </v-card-text>
    <v-card-text v-else-if="data">
      <v-row class="mb-4">
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.lastVacuum') }}
            <v-tooltip :text="t('describe.hint.lastVacuum')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div>{{ fmtAgo(data.LastVacuum) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.lastAutovacuum') }}
            <v-tooltip :text="t('describe.hint.lastAutovacuum')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div>{{ fmtAgo(data.LastAutovacuum) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.lastAnalyze') }}
            <v-tooltip :text="t('describe.hint.lastAnalyze')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div>{{ fmtAgo(data.LastAnalyze) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.lastAutoanalyze') }}
            <v-tooltip :text="t('describe.hint.lastAutoanalyze')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div>{{ fmtAgo(data.LastAutoanalyze) }}</div>
        </v-col>
      </v-row>

      <v-row class="mb-4">
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.deadTuples') }}
            <v-tooltip :text="t('describe.hint.deadTuples')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ fmtNum(data.DeadTuples) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.liveTuples') }}
            <v-tooltip :text="t('describe.hint.liveTuples')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ fmtNum(data.LiveTuples) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.modSinceAnalyze') }}
            <v-tooltip :text="t('describe.hint.modSinceAnalyze')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ fmtNum(data.ModSinceAnalyze) }}</div>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-medium-emphasis">
            {{ t('describe.insSinceVacuum') }}
            <v-tooltip :text="t('describe.hint.insSinceVacuum')" location="top" max-width="350">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <div class="text-h6">{{ fmtNum(data.InsSinceVacuum) }}</div>
        </v-col>
      </v-row>

      <div class="mb-2">
        <div class="d-flex align-center ga-2 mb-1">
          <span class="text-caption" style="min-width: 140px;">
            {{ t('describe.vacuumThreshold') }}
            <v-tooltip :text="t('describe.hint.vacuumThreshold')" location="top" max-width="400">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </span>
          <v-progress-linear
            :model-value="progressPct(data.DeadTuples, data.VacuumThreshold)"
            :color="progressColor(progressPct(data.DeadTuples, data.VacuumThreshold))"
            height="16"
            rounded
          >
            <template #default>
              <span class="text-caption">{{ fmtNum(data.DeadTuples) }} / {{ fmtNum(data.VacuumThreshold) }}</span>
            </template>
          </v-progress-linear>
        </div>
        <div class="d-flex align-center ga-2 mb-1">
          <span class="text-caption" style="min-width: 140px;">
            {{ t('describe.analyzeThreshold') }}
            <v-tooltip :text="t('describe.hint.analyzeThreshold')" location="top" max-width="400">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </span>
          <v-progress-linear
            :model-value="progressPct(data.ModSinceAnalyze, data.AnalyzeThreshold)"
            :color="progressColor(progressPct(data.ModSinceAnalyze, data.AnalyzeThreshold))"
            height="16"
            rounded
          >
            <template #default>
              <span class="text-caption">{{ fmtNum(data.ModSinceAnalyze) }} / {{ fmtNum(data.AnalyzeThreshold) }}</span>
            </template>
          </v-progress-linear>
        </div>
        <div class="d-flex align-center ga-2">
          <span class="text-caption" style="min-width: 140px;">
            {{ t('describe.insertVacThreshold') }}
            <v-tooltip :text="t('describe.hint.insertVacThreshold')" location="top" max-width="400">
              <template #activator="{ props: tp }">
                <v-icon v-bind="tp" size="x-small" class="ml-1" style="cursor: help;">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </span>
          <v-progress-linear
            :model-value="progressPct(data.InsSinceVacuum, data.InsertVacThreshold)"
            :color="progressColor(progressPct(data.InsSinceVacuum, data.InsertVacThreshold))"
            height="16"
            rounded
          >
            <template #default>
              <span class="text-caption">{{ fmtNum(data.InsSinceVacuum) }} / {{ fmtNum(data.InsertVacThreshold) }}</span>
            </template>
          </v-progress-linear>
        </div>
      </div>
    </v-card-text>
  </v-card>
</template>
