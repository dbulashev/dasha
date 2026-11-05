<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getConnectionWaitEvents } from '@/api/gen/default/default'
import type { WaitEvent } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useApiLoader } from '@/composables/useApiLoader'
import { useViewError } from '@/composables/useViewError'
import { ACTIVITY_MIN_VERSION_NUM, classColor, waitTypeClass } from '@/components/activity/classes'
import { useThemeStore } from '@/stores/theme'
import { useInstanceInfoStore } from '@/stores/instanceInfo'

const { clusterName, hostName } = useClusterInfo()
const { t } = useI18n()
const { onError } = useViewError()
const themeStore = useThemeStore()
const instanceInfoStore = useInstanceInfoStore()

const activityAvailable = computed(
  () =>
    (instanceInfoStore.known(clusterName.value ?? '', hostName.value ?? '')?.VersionNum ?? 0) >=
    ACTIVITY_MIN_VERSION_NUM,
)

const activityLink = computed(() => ({
  name: 'activity',
  params: { clustername: clusterName.value ?? '' },
  query: { host: hostName.value ?? '' },
}))

const headers = computed(() => [
  { title: t('header.waitEventType'), key: 'WaitEventType' },
  { title: t('header.waitEvent'), key: 'WaitEvent' },
  { title: t('header.amount'), key: 'Count' },
])

const { items, loading } = useApiLoader<WaitEvent[]>(
  () => getConnectionWaitEvents({
    cluster_name: clusterName.value!,
    instance: hostName.value!,
  }),
  {
    deps: [clusterName, hostName],
    guard: () => !!clusterName.value && !!hostName.value,
    onError,
  },
)

const totalWaiting = computed(() =>
  items.value.reduce((sum, e) => sum + e.Count, 0),
)

function eventTypeColor(type: string): string {
  return classColor(waitTypeClass(type), themeStore.currentTheme() === 'dark')
}
</script>

<template>
  <v-card class="h-100">
    <v-card-title class="d-flex align-center ga-2">
      <v-icon start icon="mdi-timer-sand" />
      <router-link
        v-if="activityAvailable"
        :to="activityLink"
        class="text-decoration-none text-high-emphasis"
        :title="t('activity.menu')"
      >
        {{ t('home.waitEvents') }}
        <v-icon size="x-small" icon="mdi-open-in-app" />
      </router-link>
      <span v-else>{{ t('home.waitEvents') }}</span>
      <v-chip v-if="!loading && totalWaiting > 0" size="small" variant="tonal" color="warning">
        {{ totalWaiting }}
      </v-chip>
    </v-card-title>
    <v-card-text>
      <v-alert v-if="!loading && items.length === 0" type="success" variant="tonal" class="mb-0">
        {{ t('home.noWaitEvents') }}
      </v-alert>
      <v-data-table
        v-else
        :headers="headers"
        :items="items"
        :loading="loading"
      >
        <template #item.WaitEventType="{ item }">
          <v-chip size="small" :color="eventTypeColor(item.WaitEventType)" variant="tonal">
            {{ item.WaitEventType }}
          </v-chip>
        </template>
      </v-data-table>
    </v-card-text>
  </v-card>
</template>
