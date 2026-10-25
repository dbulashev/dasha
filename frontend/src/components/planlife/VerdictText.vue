<script setup lang="ts">
import type { Verdict } from './planlife'

defineProps<{ verdict: Verdict }>()
const emit = defineEmits<{ select: [pathId: number] }>()
</script>

<template>
  <i18n-t :keypath="verdict.key" tag="span" scope="global">
    <template v-if="verdict.pathId != null" #path>
      <a href="#" @click.prevent="emit('select', verdict.pathId!)">{{ verdict.pathText }}</a>
    </template>
    <template v-for="(value, name) in verdict.params" :key="name" #[name]>{{ value }}</template>
  </i18n-t>
</template>
