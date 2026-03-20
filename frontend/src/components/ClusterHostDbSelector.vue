<template>
  <div class="d-flex align-center ga-3">

    <!-- Cluster -->
    <v-autocomplete
      v-model="selectedCluster"
      :items="clusterNames"
      :loading="loading"
      :label="t('Database Cluster')"
      density="compact"
      hide-details
      class="selector-field"
    />

    <!-- Host -->
    <v-autocomplete
      v-model="selectedHost"
      :items="hostOptions"
      :disabled="!selectedCluster"
      :loading="loading"
      :label="t('Database Host')"
      density="compact"
      hide-details
      class="selector-field"
    />

    <!-- Database -->
    <v-autocomplete
      v-model="selectedDb"
      :items="dbOptions"
      :disabled="!selectedCluster"
      :loading="loading"
      :label="t('Database')"
      density="compact"
      hide-details
      class="selector-field"
    />

  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useClusterSelector } from '@/composables/useClusterSelector'

const { t } = useI18n()
const {
  loading,
  selectedCluster,
  selectedHost,
  selectedDb,
  clusterNames,
  hostOptions,
  dbOptions,
} = useClusterSelector()
</script>

<style scoped>
.selector-field {
  min-width: 170px;
}
</style>
