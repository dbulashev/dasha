<script setup lang="ts">
import HomeSummarySection from '@/components/home/HomeSummarySection.vue'
import DatabaseHealthSection from '@/components/home/DatabaseHealthSection.vue'
import CacheHitRatesSection from '@/components/home/CacheHitRatesSection.vue'
import WaitEventsSection from '@/components/home/WaitEventsSection.vue'
import { useViewError } from '@/composables/useViewError'

const { errorMessage, onError } = useViewError()
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>
  <HomeSummarySection @error="onError" />
  <v-row class="mb-0">
    <v-col cols="12" md="6">
      <DatabaseHealthSection @error="onError" />
    </v-col>
    <v-col cols="12" md="6">
      <CacheHitRatesSection @error="onError" />
    </v-col>
  </v-row>
  <WaitEventsSection @error="onError" />
</template>
