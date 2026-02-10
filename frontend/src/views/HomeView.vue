<script setup lang="ts">
import { ref } from 'vue'
import HomeSummarySection from '@/components/home/HomeSummarySection.vue'
import CacheHitRatesSection from '@/components/home/CacheHitRatesSection.vue'
import ConnectionStatesSection from '@/components/home/ConnectionStatesSection.vue'
import ConnectionSourcesSection from '@/components/home/ConnectionSourcesSection.vue'
import StatActivitySection from '@/components/home/StatActivitySection.vue'

const errorMessage = ref('')
function onError(msg: string) { errorMessage.value = msg }
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>
  <HomeSummarySection @error="onError" />
  <CacheHitRatesSection @error="onError" />
  <ConnectionStatesSection @error="onError" />
  <ConnectionSourcesSection @error="onError" />
  <StatActivitySection @error="onError" />
</template>
