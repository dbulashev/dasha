<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { copyToClipboard, highlightJson, isJson, prettyJson } from '@/utils/sql'
import '@/assets/sql-highlight.css'

const props = defineProps<{
  modelValue: boolean
  plan: string
  queryId?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const { t } = useI18n()

// A json plan is stored as one line; the text format already carries its own.
const body = computed(() => prettyJson(props.plan))

const markup = computed(() => (isJson(props.plan) ? highlightJson(body.value) : ''))
</script>

<template>
  <v-dialog
    :model-value="props.modelValue"
    max-width="1100"
    scrollable
    @update:model-value="emit('update:modelValue', $event)"
  >
    <v-card>
      <v-card-title class="d-flex align-center">
        <span>{{ t('logs.planDialog.title') }}</span>
        <span v-if="props.queryId" class="text-caption text-medium-emphasis ms-2">
          queryid: {{ props.queryId }}
        </span>
        <v-spacer />
        <v-btn
          icon="mdi-content-copy"
          variant="text"
          size="small"
          :title="t('logs.copy.plan')"
          @click="copyToClipboard(body)"
        />
        <v-btn icon="mdi-close" variant="text" size="small" @click="emit('update:modelValue', false)" />
      </v-card-title>
      <v-card-text>
        <pre v-if="markup" class="sql-highlight sql-code text-mono text-body-2" v-html="markup"></pre>
        <pre v-else class="sql-code text-mono text-body-2">{{ body }}</pre>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>
