<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useQueryScope } from '@/composables/useQueryScope'

// Locked to "instance" while a snapshot that predates per-database attribution
// is open: its rows name no database, so narrowing would empty the page.
const props = defineProps<{ forceInstance?: boolean }>()

const { t } = useI18n()
const { scope, hasScopeChoice } = useQueryScope()
</script>

<template>
  <v-tooltip v-if="hasScopeChoice" :text="props.forceInstance ? t('scope.legacySnapshot') : t('scope.hint')" location="bottom" max-width="420">
    <template #activator="{ props: tp }">
      <v-btn-toggle
        v-bind="tp"
        :model-value="props.forceInstance ? 'instance' : scope"
        :disabled="props.forceInstance"
        density="compact"
        variant="outlined"
        divided
        mandatory
        @update:model-value="scope = $event"
      >
        <v-btn value="database" size="small">{{ t('scope.database') }}</v-btn>
        <v-btn value="instance" size="small">{{ t('scope.instance') }}</v-btn>
      </v-btn-toggle>
    </template>
  </v-tooltip>
</template>
