<script setup lang="ts">
import HealthScoreSection from '@/components/home/HealthScoreSection.vue'
import HomeSummarySection from '@/components/home/HomeSummarySection.vue'
import CacheHitRatesSection from '@/components/home/CacheHitRatesSection.vue'
import WaitEventsSection from '@/components/home/WaitEventsSection.vue'
</script>

<template>
  <HomeSummarySection />
  <HealthScoreSection class="mb-4" />
  <v-row class="mb-0">
    <v-col cols="12" md="6">
      <CacheHitRatesSection />
    </v-col>
    <v-col cols="12" md="6">
      <WaitEventsSection />
    </v-col>
  </v-row>
</template>
