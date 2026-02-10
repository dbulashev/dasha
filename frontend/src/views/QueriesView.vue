<script setup lang="ts">
import { ref, watch } from 'vue'
import { useClusterInfo } from '@/composables/useClusterInfo'
import RunningQueriesSection from '@/components/queries/RunningQueriesSection.vue'

const { clusterName, hostName, databaseName } = useClusterInfo()

const errorMessage = ref('')
function onError(msg: string) { errorMessage.value = msg }

watch([clusterName, hostName, databaseName], () => {
  errorMessage.value = ''
}, { immediate: true })
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>

  <RunningQueriesSection @error="onError" />
</template>
