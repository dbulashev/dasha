<script setup lang="ts">
import ConnectionStatesSection from '@/components/home/ConnectionStatesSection.vue'
import ConnectionSourcesSection from '@/components/home/ConnectionSourcesSection.vue'
import StatActivitySection from '@/components/home/StatActivitySection.vue'
import { useViewError } from '@/composables/useViewError'

const { errorMessage, onError } = useViewError()
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>
  <ConnectionStatesSection @error="onError" />
  <ConnectionSourcesSection @error="onError" />
  <StatActivitySection @error="onError" />
</template>
