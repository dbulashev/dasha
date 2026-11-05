<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { useThemeStore } from '@/stores/theme'
import { CLASSES, classFill } from './classes'

const props = defineProps<{
  byClass: number[]
}>()

const { t } = useI18n()
const themeStore = useThemeStore()

const parts = computed(() => {
  const total = props.byClass.reduce((a, b) => a + b, 0)
  if (!total) return []
  const dark = themeStore.currentTheme() === 'dark'
  return CLASSES.map((cls, i) => ({
    cls,
    label: cls === 'ClientIdleTx' ? t('activity.class.ClientIdleTx') : cls,
    pct: (props.byClass[i] * 100) / total,
    fill: classFill(cls, dark),
  })).filter((p) => p.pct > 0)
})

const listed = computed(() => [...parts.value].sort((a, b) => b.pct - a.pct))
</script>

<template>
  <v-tooltip location="start" :open-delay="0" :close-delay="0" :disabled="!parts.length">
    <template #activator="{ props: tip }">
      <div v-bind="tip" class="class-bar">
        <span v-for="p in parts" :key="p.cls" :style="{ width: `${p.pct}%`, background: p.fill }" />
      </div>
    </template>
    <div v-for="p in listed" :key="p.cls" class="d-flex align-center ga-2">
      <span class="swatch" :style="{ background: p.fill }" />
      <span class="flex-grow-1">{{ p.label }}</span>
      <span class="pct">{{ p.pct.toFixed(0) }}%</span>
    </div>
  </v-tooltip>
</template>

<style scoped>
.class-bar {
  display: flex;
  gap: 2px;
  width: 120px;
  height: 10px;
  border-radius: 2px;
  overflow: hidden;
  background: rgba(128, 128, 128, 0.15);
}

.swatch {
  width: 10px;
  height: 10px;
  border-radius: 2px;
  flex-shrink: 0;
}

.pct {
  min-width: 3em;
  text-align: right;
  font-variant-numeric: tabular-nums;
}
</style>
