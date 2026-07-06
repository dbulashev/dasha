<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClusterInfo } from '@/composables/useClusterInfo'
import LogSearchSection from '@/components/logs/LogSearchSection.vue'

const { t } = useI18n()
const { clusterName, currentCluster } = useClusterInfo()

const supportsLogs = computed(() => !!currentCluster.value?.supports_logs)
</script>

<template>
  <v-alert v-if="!clusterName" type="info" variant="tonal">
    {{ t('logs.selectCluster') }}
  </v-alert>
  <v-alert v-else-if="!supportsLogs" type="info" variant="tonal">
    {{ t('logs.notSupported') }}
  </v-alert>
  <LogSearchSection v-else :key="clusterName" />
</template>
