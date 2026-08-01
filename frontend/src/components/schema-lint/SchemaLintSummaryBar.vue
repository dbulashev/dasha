<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SchemaLintSummary } from '@/api/models/index'
import { GetSchemaLintLevel } from '@/api/models/index'
import { LEVEL_COLOR, LEVEL_ICON } from './levels'

const props = defineProps<{
  summary: SchemaLintSummary
  activeLevel: GetSchemaLintLevel | null
  durationMs: number
}>()

const emit = defineEmits<{ pick: [level: GetSchemaLintLevel] }>()

const { t } = useI18n()

const counters = computed(() => [
  { level: GetSchemaLintLevel.error, count: props.summary.error },
  { level: GetSchemaLintLevel.warning, count: props.summary.warning },
  { level: GetSchemaLintLevel.notice, count: props.summary.notice },
])

const clean = computed(() => counters.value.every((c) => c.count === 0))
</script>

<template>
  <div class="d-flex align-center ga-2 flex-wrap mb-3">
    <v-chip
      v-for="c in counters"
      :key="c.level"
      :color="LEVEL_COLOR[c.level]"
      :prepend-icon="LEVEL_ICON[c.level]"
      :variant="activeLevel === c.level ? 'flat' : 'tonal'"
      link
      @click="emit('pick', c.level)"
    >
      {{ t(`schemaLint.levels.${c.level}`) }}: {{ c.count }}
    </v-chip>

    <v-chip v-if="clean" color="success" variant="tonal" prepend-icon="mdi-check-circle-outline">
      {{ t('schemaLint.page.clean') }}
    </v-chip>

    <v-spacer />

    <span v-if="durationMs > 0" class="text-caption text-medium-emphasis">
      {{ t('schemaLint.page.duration', { ms: durationMs }) }}
    </span>
    <span v-else class="text-caption text-medium-emphasis">
      {{ t('schemaLint.page.fromCache') }}
    </span>
  </div>
</template>
