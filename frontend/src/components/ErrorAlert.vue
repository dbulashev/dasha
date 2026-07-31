<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { statusMessageKey } from '@/utils/error'

const { t } = useI18n()

const props = defineProps<{
  code: number
  message: string
}>()

const text = computed(() => {
  const key = statusMessageKey(props.code)
  return key ? t(key) : props.message
})
</script>

<template>
  <v-empty-state
    :headline="String(code)"
    :title="t('Something went wrong')"
    :text="text"
    image="/dasha-appology.png"
  />
</template>
