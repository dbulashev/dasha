<script setup lang="ts">
import SettingsAnalyzeSection from '@/components/settings/SettingsAnalyzeSection.vue'
import PgSettingsSection from '@/components/settings/PgSettingsSection.vue'
import AutovacuumSettingsSection from '@/components/settings/AutovacuumSettingsSection.vue'
import { useViewError } from '@/composables/useViewError'

const { errorMessage, onError } = useViewError()
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>
  <SettingsAnalyzeSection @error="onError" />
  <PgSettingsSection @error="onError" />
  <AutovacuumSettingsSection @error="onError" />
</template>
