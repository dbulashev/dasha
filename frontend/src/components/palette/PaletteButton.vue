<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { modKeyLabel } from '@/utils/palette/keys'

defineEmits<{ click: [] }>()

const { t } = useI18n()
const label = computed(() => t('palette.button', { key: `${modKeyLabel()}K` }))
</script>

<template>
  <v-btn icon="mdi-magnify" :aria-label="label" v-tooltip="label" slim @click="$emit('click')" />
</template>
