<script setup lang="ts">
import HealthScoreSection from '@/components/home/HealthScoreSection.vue'
import HomeSummarySection from '@/components/home/HomeSummarySection.vue'
import CacheHitRatesSection from '@/components/home/CacheHitRatesSection.vue'
import WaitEventsSection from '@/components/home/WaitEventsSection.vue'
import { useAuthStore } from '@/stores/auth'

// Health Score is admin-only while the model is being calibrated/validated.
const authStore = useAuthStore()
</script>

<template>
  <HomeSummarySection />
  <HealthScoreSection v-if="authStore.isAdmin" class="mb-4" />
  <v-row class="mb-0">
    <v-col cols="12" md="6">
      <CacheHitRatesSection />
    </v-col>
    <v-col cols="12" md="6">
      <WaitEventsSection />
    </v-col>
  </v-row>
</template>
