<script setup lang="ts">
import AutovacuumQueueSection from '@/components/maintenance/AutovacuumQueueSection.vue'
import AutovacuumSummarySection from '@/components/maintenance/AutovacuumSummarySection.vue'
import TransactionIdDangerSection from '@/components/maintenance/TransactionIdDangerSection.vue'
import VacuumProgressSection from '@/components/maintenance/VacuumProgressSection.vue'
import MaintenanceInfoSection from '@/components/maintenance/MaintenanceInfoSection.vue'
</script>

<template>
  <!-- <FreezeMaxAgeSection /> -->
  <AutovacuumSummarySection />
  <AutovacuumQueueSection />
  <TransactionIdDangerSection />
  <VacuumProgressSection />
  <MaintenanceInfoSection />
</template>
