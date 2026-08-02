<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SchemaLintSkip } from '@/api/models/index'

const props = defineProps<{ skipped: SchemaLintSkip[] }>()

const { t, te } = useI18n()

// A check switched off in the config is a decision, not a gap: keep it out of the
// warning so the block stays about checks that should have run and did not.
const notable = computed(() => props.skipped.filter((s) => s.reason !== 'disabled'))

function checkTitle(code: string) {
  const key = `schemaLint.checks.${code}.title`
  return te(key) ? t(key) : code
}

// The backend sends a reason code, an object count and at most a technical
// marker (a SQLSTATE, "timeout") — the sentence is built here so it follows the
// interface language.
function reasonText(skip: SchemaLintSkip) {
  const key = `schemaLint.skipReasons.${skip.reason}`
  const parts = [te(key) ? t(key) : skip.reason]

  if (skip.count) parts.push(t('schemaLint.page.skippedObjects', { count: skip.count }, skip.count))
  if (skip.detail) parts.push(`(${skip.detail})`)

  return parts.join(' ')
}
</script>

<template>
  <v-alert
    v-if="notable.length"
    type="warning"
    variant="tonal"
    density="compact"
    class="mb-3"
    icon="mdi-help-rhombus-outline"
  >
    <div class="font-weight-medium mb-1">{{ t('schemaLint.page.skippedTitle') }}</div>
    <div class="text-caption mb-2">{{ t('schemaLint.page.skippedHint') }}</div>
    <div v-for="skip in notable" :key="`${skip.code}:${skip.reason}`" class="text-body-2">
      <span class="font-weight-medium">{{ checkTitle(skip.code) }}</span> — {{ reasonText(skip) }}
      <span v-if="skip.required_privilege" class="text-caption text-medium-emphasis">
        {{ t('schemaLint.page.grantHint', { privilege: skip.required_privilege }) }}
      </span>
    </div>
  </v-alert>
</template>
