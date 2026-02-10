<script setup lang="ts">
import TransactionIdDangerSection from '@/components/maintenance/TransactionIdDangerSection.vue'
import VacuumProgressSection from '@/components/maintenance/VacuumProgressSection.vue'
import MaintenanceInfoSection from '@/components/maintenance/MaintenanceInfoSection.vue'
import { useViewError } from '@/composables/useViewError'

const { errorMessage, onError } = useViewError()
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>
  <!-- <FreezeMaxAgeSection @error="onError" /> -->
  <TransactionIdDangerSection @error="onError" />
  <VacuumProgressSection @error="onError" />
  <MaintenanceInfoSection @error="onError" />
</template>
