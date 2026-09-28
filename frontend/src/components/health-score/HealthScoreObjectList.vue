<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { recObjectName, type RecObject } from './recObjects'

defineProps<{
  objects: RecObject[]
  more: number
}>()

defineSlots<{
  value?: (props: { obj: RecObject }) => unknown
}>()

const { t } = useI18n()
</script>

<template>
  <div class="text-body-2">
    <div v-for="obj in objects" :key="recObjectName(obj)" class="d-flex align-baseline ga-2">
      <span class="rec-object-name">{{ recObjectName(obj) }}</span>
      <span class="text-medium-emphasis">
        <slot name="value" :obj="obj" />
      </span>
    </div>
    <div v-if="more > 0" class="text-medium-emphasis">
      {{ t('healthScore.hot.more', { n: more }) }}
    </div>
  </div>
</template>

<style scoped>
.rec-object-name {
  font-family: 'Roboto Mono', ui-monospace, monospace;
  font-size: 0.9em;
  word-break: break-all;
}
</style>
