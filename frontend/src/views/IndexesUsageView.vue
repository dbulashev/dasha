<script setup lang="ts">
import UsageSection from '@/components/indexes/UsageSection.vue'
import UnusedSection from '@/components/indexes/UnusedSection.vue'
import { useViewError } from '@/composables/useViewError'

const { errorMessage, onError } = useViewError()
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>
    {{ errorMessage }}
  </v-alert>

  <UnusedSection @error="onError" />
  <UsageSection @error="onError" />
</template>
