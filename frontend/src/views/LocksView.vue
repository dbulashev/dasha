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

const { clusterName, databaseName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError, clearError } = useViewError()

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
    })
    blockedItems.value = assertOk(response) ?? []
  } catch (err) {
    onError(getErrorMessage(err), err)
    blockedItems.value = []
  } finally {
    blockedLoading.value = false
  }
}

watch([clusterName, hostName, databaseName], () => {
  loadBlocked()
}, { immediate: true })
</script>

<template>

  <!-- Lock Tree Visualization -->
  <v-card class="mb-4">
    <v-card-title><v-icon start icon="mdi-lock-outline" />{{ t('locks.tree') }}</v-card-title>
    <v-card-text>
      <LockTree :items="blockedItems" :loading="blockedLoading" />
    </v-card-text>
  </v-card>

</template>
