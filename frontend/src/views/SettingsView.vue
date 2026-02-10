<script setup lang="ts">
import { ref } from 'vue'
import SettingsAnalyzeSection from '@/components/settings/SettingsAnalyzeSection.vue'
import PgSettingsSection from '@/components/settings/PgSettingsSection.vue'
import AutovacuumSettingsSection from '@/components/settings/AutovacuumSettingsSection.vue'

const errorMessage = ref('')
function onError(msg: string) { errorMessage.value = msg }
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>
  <SettingsAnalyzeSection @error="onError" />
  <PgSettingsSection @error="onError" />
  <AutovacuumSettingsSection @error="onError" />
</template>
