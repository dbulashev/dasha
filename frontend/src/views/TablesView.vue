<script setup lang="ts">
import { ref } from 'vue'
import TopKBySizeSection from '@/components/tables/TopKBySizeSection.vue'
import CachingSection from '@/components/tables/CachingSection.vue'
import PartitionsSection from '@/components/tables/PartitionsSection.vue'

const errorMessage = ref('')
function onError(msg: string) { errorMessage.value = msg }
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>
  <TopKBySizeSection @error="onError" />
  <CachingSection @error="onError" />
  <PartitionsSection @error="onError" />
</template>
