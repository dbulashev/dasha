<template>
  <div class="d-flex align-center" style="gap: 10px">

    <!-- Cluster -->
    <v-autocomplete
      v-model="selectedCluster"
      :items="clusterNames"
      :loading="loading"
      :label="t('Database Cluster')"
      density="compact"
      hide-details
      style="min-width: 170px"
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
      style="min-width: 170px"
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
      style="min-width: 170px"
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
