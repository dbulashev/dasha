<script setup lang="ts">
import { watch } from 'vue'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useViewError } from '@/composables/useViewError'
import RunningQueriesSection from '@/components/queries/RunningQueriesSection.vue'

const { clusterName, hostName, databaseName } = useClusterInfo()
const { errorMessage, onError, clearError } = useViewError()

watch([clusterName, hostName, databaseName], clearError, { immediate: true })
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>

  <RunningQueriesSection @error="onError" />
</template>
