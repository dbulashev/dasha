<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

defineProps<{
  code: number
  message: string
}>()
</script>

<template>
  <v-empty-state
    :headline="String(code)"
    :title="t('Something went wrong')"
    :text="message"
    icon="mdi-alert-circle-outline"
  />
</template>
