<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanLifecycleChainNode, PlanLifecyclePath } from '@/api/models'
import { fmtCompactFloat } from '@/utils/format'
import { causeText, fateColor, paramText, pathName } from './planlife'

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

const rowsText = computed(() => {
  if (p.value?.rows == null) return ''
  const rows = t('planLifecycle.rowsShort', { rows: fmtCompactFloat(p.value.rows) })
  return p.value.required_outer?.length ? `${rows} ${t('planLifecycle.perLoop')}` : rows
})
</script>

<template>
  <div v-if="p" :style="{ paddingLeft: `${depth * 20}px` }" class="text-body-2 py-1">
    <span v-if="depth > 0" class="text-medium-emphasis">↑ </span>
    <a href="#" @click.prevent="emit('select', p.path_id)">{{ pathName(p, aliases, t, true) }} #{{ p.path_id }}</a>
    <span v-if="paramText(p, aliases)" class="text-medium-emphasis">
      · {{ t('planLifecycle.paramFrom', { rels: paramText(p, aliases) }) }}
    </span>
    <span v-if="rowsText" class="text-medium-emphasis"> · {{ rowsText }}</span>
    <v-chip :color="fateColor(p.fate)" :title="t(`planLifecycle.fateHint.${p.fate}`)" size="x-small" variant="flat" label class="ms-2">
      {{ t(`planLifecycle.fate.${p.fate}`) }}
    </v-chip>
    <span v-if="winner" class="ms-2">
      {{ t(p.fate === 'removed' ? 'planLifecycle.removedBy' : 'planLifecycle.lostTo') }}
      <a href="#" @click.prevent="emit('select', p.by!)">{{ pathName(winner, aliases, t, true) }} #{{ p.by }}</a>
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
