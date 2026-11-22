<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type { PaletteItem } from '@/composables/usePaletteItems'

const props = defineProps<{
  item: PaletteItem
  id: string
  selected: boolean
}>()

defineEmits<{ pick: [event: MouseEvent | KeyboardEvent] }>()

const { t } = useI18n()

const segments = computed(() => {
  const { title, titleMarks } = props.item
  const out: { text: string; mark: boolean }[] = []
  let at = 0
  for (const [start, end] of titleMarks) {
    if (start > at) out.push({ text: title.slice(at, start), mark: false })
    out.push({ text: title.slice(start, end), mark: true })
    at = end
  }
  if (at < title.length) out.push({ text: title.slice(at), mark: false })
  return out
})
</script>

<template>
  <!-- mousedown is prevented so that the focus stays in the search field. -->
  <v-list-item
    :id="id"
    role="option"
    :aria-selected="selected"
    :active="selected"
    :class="{ 'palette-row--current': item.current }"
    :prepend-icon="item.icon"
    density="compact"
    @mousedown.prevent
    @click="$emit('pick', $event)"
  >
    <v-list-item-title>
      <template v-for="(s, i) in segments" :key="i"><mark v-if="s.mark" class="palette-mark">{{ s.text }}</mark><template v-else>{{ s.text }}</template></template>
      <v-chip v-if="item.current" size="x-small" variant="tonal" class="ml-2">{{ t('palette.here') }}</v-chip>
    </v-list-item-title>
    <v-list-item-subtitle v-if="item.subtitle.length">{{ item.subtitle.join(' · ') }}</v-list-item-subtitle>
    <template #append>
      <kbd v-if="item.digit !== undefined" class="palette-kbd">{{ item.digit }}</kbd>
      <kbd v-else-if="selected" class="palette-kbd">↵</kbd>
    </template>
  </v-list-item>
</template>

<style scoped>
.palette-mark {
  background: rgba(var(--v-theme-primary), 0.24);
  color: inherit;
  border-radius: 2px;
}

.palette-row--current :deep(.v-list-item-title) {
  opacity: var(--v-medium-emphasis-opacity);
}

.palette-kbd {
  min-width: 1.5em;
  padding: 0 0.35em;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 4px;
  font-family: inherit;
  font-size: 0.75rem;
  text-align: center;
}
</style>
