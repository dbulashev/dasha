<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import FleetHealthSection from '@/components/fleet/FleetHealthSection.vue'

const { t } = useI18n()
</script>

<template>
  <div>
    <h1 class="text-h5 mb-4 d-flex align-center ga-2">
      <v-icon icon="mdi-server-network" />
      {{ t('fleetHealth.title') }}
    </h1>

    <FleetHealthSection />
  </div>
</template>
