<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanLifecycleChainNode, PlanLifecyclePath } from '@/api/models'
import { causeText, fateColor, paramText, pathTypeLabel } from './planlife'

const props = defineProps<{
  node: PlanLifecycleChainNode
  paths: Map<number, PlanLifecyclePath>
  aliases: Map<number, string>
  depth: number
}>()

const emit = defineEmits<{ select: [pathId: number] }>()

const { t } = useI18n()

const p = computed(() => props.paths.get(props.node.path_id))
const winner = computed(() => (p.value?.by != null ? props.paths.get(p.value.by) : undefined))
</script>

<template>
  <div v-if="p" :style="{ paddingLeft: `${depth * 20}px` }" class="text-body-2 py-1">
    <span v-if="depth > 0" class="text-medium-emphasis">└ </span>
    <a href="#" @click.prevent="emit('select', p.path_id)">{{ pathTypeLabel(p.pathtype) }} #{{ p.path_id }}</a>
    <span v-if="paramText(p, aliases)" class="text-medium-emphasis">
      · {{ t('planLifecycle.paramFrom', { rels: paramText(p, aliases) }) }}
    </span>
    <span v-if="p.rows != null" class="text-medium-emphasis">
      · {{ t('planLifecycle.rowsShort', { rows: p.rows }) }}<template v-if="p.required_outer?.length"> {{ t('planLifecycle.perLoop') }}</template>
    </span>
    <v-chip :color="fateColor(p.fate)" size="x-small" variant="flat" label class="ms-2">
      {{ t(`planLifecycle.fate.${p.fate}`) }}
    </v-chip>
    <span v-if="winner" class="ms-2">
      {{ t(p.fate === 'removed' ? 'planLifecycle.removedBy' : 'planLifecycle.lostTo') }}
      <a href="#" @click.prevent="emit('select', p.by!)">{{ pathTypeLabel(winner.pathtype) }} #{{ p.by }}</a>
    </span>
    <span v-if="p.cause" class="text-caption text-medium-emphasis ms-2">{{ causeText(p.cause, t) }}</span>
  </div>
  <ChainNodeView
    v-for="up in node.up"
    :key="up.path_id"
    :node="up"
    :paths="paths"
    :aliases="aliases"
    :depth="depth + 1"
    @select="id => emit('select', id)"
  />
</template>
