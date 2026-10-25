<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanLifecycleAnalysis } from '@/api/models'
import ChainNodeView from './ChainNodeView.vue'
import { pathsById, relAliases } from './planlife'

const props = defineProps<{ analysis: PlanLifecycleAnalysis }>()
const emit = defineEmits<{ select: [pathId: number] }>()

const { t } = useI18n()

const paths = computed(() => pathsById(props.analysis))
const aliases = computed(() => relAliases(props.analysis))

const OUTCOME_COLOR: Record<string, string> = { in_plan: 'success', lost: 'warning', only_rejected: 'error' }
</script>

<template>
  <div>
    <v-alert type="info" variant="tonal" density="compact" class="mb-3">
      {{ t('planLifecycle.caveat.index_not_examined_unobservable') }}
    </v-alert>

    <div v-if="!analysis.indexes.length" class="text-medium-emphasis">{{ t('planLifecycle.noIndexes') }}</div>

    <v-expansion-panels multiple variant="accordion">
      <v-expansion-panel v-for="ix in analysis.indexes" :key="ix.index">
        <v-expansion-panel-title>
          <span class="text-mono me-2">{{ ix.index }}</span>
          <v-chip v-if="ix.hypothetical" size="x-small" label class="me-2">{{ t('planLifecycle.hypotheticalIndex') }}</v-chip>
          <v-chip :color="OUTCOME_COLOR[ix.outcome]" size="x-small" variant="flat" label>
            {{ t(`planLifecycle.outcome.${ix.outcome}`) }}
          </v-chip>
        </v-expansion-panel-title>
        <v-expansion-panel-text>
          <v-alert v-if="ix.truncated" type="warning" variant="tonal" density="compact" class="mb-2">
            {{ t('planLifecycle.chainTruncated') }}
          </v-alert>
          <ChainNodeView
            v-for="leaf in ix.leaves"
            :key="leaf.path_id"
            :node="leaf"
            :paths="paths"
            :aliases="aliases"
            :depth="0"
            @select="id => emit('select', id)"
          />
        </v-expansion-panel-text>
      </v-expansion-panel>
    </v-expansion-panels>
  </div>
</template>
