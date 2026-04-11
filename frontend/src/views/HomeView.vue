<script setup lang="ts">
import HomeSummarySection from '@/components/home/HomeSummarySection.vue'
import DatabaseHealthSection from '@/components/home/DatabaseHealthSection.vue'
import CacheHitRatesSection from '@/components/home/CacheHitRatesSection.vue'
import WaitEventsSection from '@/components/home/WaitEventsSection.vue'
</script>

<template>
  <HomeSummarySection />
  <v-row class="mb-0">
    <v-col cols="12" md="6">
      <DatabaseHealthSection />
    </v-col>
    <v-col cols="12" md="6">
      <CacheHitRatesSection />
    </v-col>
  </v-row>
  <WaitEventsSection />
</template>
