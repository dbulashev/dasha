<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQueriesBlocked } from '@/api/gen/default/default'
import type { QueryBlocked } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useViewError } from '@/composables/useViewError'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import LockTree from '@/components/locks/LockTree.vue'
import ScopeSwitch from '@/components/queries/ScopeSwitch.vue'
import { useQueryScope } from '@/composables/useQueryScope'

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError, clearError } = useViewError()
const { scope, isInstanceScope } = useQueryScope()

// --- Blocked queries (locks) ---
const blockedItems = ref<QueryBlocked[]>([])
const blockedLoading = ref(false)

async function loadBlocked() {
  if (!clusterName.value || !hostName.value || !databaseName.value) return
  blockedLoading.value = true
  clearError()
  try {
    const response = await getQueriesBlocked({
      cluster_name: clusterName.value,
      instance: hostName.value,
      database: databaseName.value,
      scope: scope.value,
    })
    blockedItems.value = assertOk(response) ?? []
  } catch (err) {
    onError(getErrorMessage(err), err)
    blockedItems.value = []
  } finally {
    blockedLoading.value = false
  }
}

watch([clusterName, hostName, databaseName, scope], () => {
  loadBlocked()
}, { immediate: true })
</script>

<template>

  <!-- Lock Tree Visualization -->
  <v-card class="mb-4">
    <v-card-title class="d-flex align-center ga-2">
      <span><v-icon start icon="mdi-lock-outline" />{{ t('locks.tree') }}</span>
      <ScopeSwitch />
      <v-tooltip v-if="isInstanceScope" :text="t('locks.foreignObjectHint')" location="bottom" max-width="420">
        <template #activator="{ props }">
          <v-icon v-bind="props" size="small" color="medium-emphasis">mdi-help-circle-outline</v-icon>
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <LockTree :items="blockedItems" :loading="blockedLoading" :show-database="isInstanceScope" />
    </v-card-text>
  </v-card>

</template>
