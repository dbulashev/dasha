<script setup lang="ts">
import HomeSummarySection from '@/components/home/HomeSummarySection.vue'
import CacheHitRatesSection from '@/components/home/CacheHitRatesSection.vue'
import { useViewError } from '@/composables/useViewError'

const { errorMessage, onError } = useViewError()
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>
  <HomeSummarySection @error="onError" />
  <CacheHitRatesSection @error="onError" />
</template>
