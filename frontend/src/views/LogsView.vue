<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClusterInfo } from '@/composables/useClusterInfo'
import LogSearchSection from '@/components/logs/LogSearchSection.vue'

const { t } = useI18n()
const { clusterName, currentCluster } = useClusterInfo()

const isYandex = computed(() => currentCluster.value?.source === 'yandex-mdb')
</script>

<template>
  <v-alert v-if="!clusterName" type="info" variant="tonal">
    {{ t('logs.selectCluster') }}
  </v-alert>
  <v-alert v-else-if="!isYandex" type="info" variant="tonal">
    {{ t('logs.notYandex') }}
  </v-alert>
  <LogSearchSection v-else :key="clusterName" />
</template>
