<script setup lang="ts">
defineProps<{
  page: number
  hasMore: boolean
}>()

const emit = defineEmits<{
  'update:page': [page: number]
}>()
</script>

<template>
  <div v-if="page > 1 || hasMore" class="d-flex justify-center align-center ga-2 pa-2">
    <v-btn icon="mdi-chevron-left" variant="text" size="small" :disabled="page <= 1" @click="emit('update:page', page - 1)" />
    <span class="text-body-2">{{ page }}</span>
    <v-btn icon="mdi-chevron-right" variant="text" size="small" :disabled="!hasMore" @click="emit('update:page', page + 1)" />
  </div>
</template>
