<script setup lang="ts">
import ReplicationConfigSection from '@/components/replication/ReplicationConfigSection.vue'
import ReplicationStatusSection from '@/components/replication/ReplicationStatusSection.vue'
import ReplicationSlotsSection from '@/components/replication/ReplicationSlotsSection.vue'
import { useViewError } from '@/composables/useViewError'

const { errorMessage, onError } = useViewError()
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>
  <ReplicationConfigSection @error="onError" />
  <ReplicationStatusSection @error="onError" />
  <ReplicationSlotsSection @error="onError" />
</template>
