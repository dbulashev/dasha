<script setup lang="ts">
// narrowed_by of a log read: what the log store filtered on before Dasha scanned.
const props = defineProps<{
  items: string[]
}>()
</script>

<template>
  <v-chip
    v-for="n in props.items"
    :key="n"
    size="small"
    variant="tonal"
    label
    class="text-mono"
  >
    {{ n }}
  </v-chip>
</template>
