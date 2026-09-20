<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanDormantRule, PlanFinding } from '@/api/models'
import { findingText, severityColor, severityRank } from '@/components/explain/planText'

const props = defineProps<{
  findings: PlanFinding[]
  dormant?: PlanDormantRule[]
  selected?: number[] | null
}>()

const emit = defineEmits<{
  select: [path: number[]]
}>()

const { t, te } = useI18n()

const sorted = computed(() =>
  [...props.findings].sort((a, b) => severityRank(a.severity) - severityRank(b.severity)),
)

const dormantText = computed(() =>
  (props.dormant ?? []).map(d => {
    const missing = (d.missing ?? []).map(m => t(`plan.missing.${m}`)).join(', ')
    return t('plan.dormantRule', { code: d.code, missing })
  }),
)

function isSelected(f: PlanFinding): boolean {
  return !!props.selected && (f.path ?? []).join('.') === props.selected.join('.')
}
</script>

<template>
  <div class="d-flex align-center flex-wrap ga-1">
    <v-chip
      v-for="(f, i) in sorted"
      :key="i"
      :color="severityColor(f.severity)"
      :variant="isSelected(f) ? 'flat' : 'tonal'"
      size="small"
      label
      @click="emit('select', f.path ?? [])"
    >
      {{ findingText(f, t, te) }}
      <span class="text-caption ms-1">· {{ f.node_type }}</span>
    </v-chip>

    <span v-if="!sorted.length" class="text-caption text-medium-emphasis">
      {{ t('plan.noFindings') }}
    </span>

    <span v-for="(line, i) in dormantText" :key="`d${i}`" class="text-caption text-medium-emphasis ms-2">
      {{ line }}
    </span>
  </div>
</template>
