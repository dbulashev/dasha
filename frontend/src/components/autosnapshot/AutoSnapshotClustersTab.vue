<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAutosnapshotCluster } from '@/api/gen/default/default'
import type { AutoSnapshotClusterOverride } from '@/api/models'
import { useClustersStore } from '@/stores/clusters'
import { useDebouncedRef } from '@/composables/useDebouncedRef'
import { assertOk } from '@/utils/api'
import AutoSnapshotClusterDialog from './AutoSnapshotClusterDialog.vue'

const { t } = useI18n()
const clustersStore = useClustersStore()

type ClusterRow = {
  ClusterName: string
  HasOverride: boolean
  ActivitySpikeEnabled: boolean
  RoleChangeEnabled: boolean
  LoadError: string | null
}

const items = ref<ClusterRow[]>([])
const loading = ref(false)
const editCluster = ref<string | null>(null)
const dialogOpen = ref(false)
const search = ref<string | null>('')
const debouncedSearch = useDebouncedRef(search, 200)

const filteredItems = computed(() => {
  // `clearable` sets the model to null on clear — guard before trim().
  const q = (debouncedSearch.value ?? '').trim().toLowerCase()
  return q ? items.value.filter((r) => r.ClusterName.toLowerCase().includes(q)) : items.value
})

const headers = computed(() => [
  { title: t('autosnapshot.clusters.name'), key: 'ClusterName' },
  { title: t('autosnapshot.clusters.status'), key: 'Status', sortable: false },
  { title: t('autosnapshot.clusters.triggers'), key: 'Triggers', sortable: false },
])

function rowFromOverride(name: string, body: AutoSnapshotClusterOverride): ClusterRow {
  const overrides = (body?.Overrides ?? {}) as Record<string, unknown>
  const hasOverride = Object.keys(overrides).length > 0
  return {
    ClusterName: name,
    HasOverride: hasOverride,
    ActivitySpikeEnabled: !!body?.Effective?.ActivitySpike?.Enabled,
    RoleChangeEnabled: !!body?.Effective?.RoleChange?.Enabled,
    LoadError: null,
  }
}

function errorRow(name: string, msg: string): ClusterRow {
  return {
    ClusterName: name,
    HasOverride: false,
    ActivitySpikeEnabled: false,
    RoleChangeEnabled: false,
    LoadError: msg,
  }
}

async function fetchOne(name: string): Promise<ClusterRow> {
  try {
    const res = await getAutosnapshotCluster(name)
    const body = assertOk<AutoSnapshotClusterOverride>(res)
    return rowFromOverride(name, body)
  } catch (e) {
    return errorRow(name, String(e))
  }
}

async function loadAll() {
  const clusters = clustersStore.clusterList ?? []
  const names = clusters
    .map((c) => c.name ?? '')
    .filter((n): n is string => !!n)
  if (!names.length) {
    items.value = []
    return
  }
  loading.value = true
  try {
    const results = await Promise.all(names.map((n) => fetchOne(n)))
    items.value = results
  } finally {
    loading.value = false
  }
}

async function refreshOne(name: string) {
  const row = await fetchOne(name)
  const idx = items.value.findIndex((r) => r.ClusterName === name)
  if (idx >= 0) items.value.splice(idx, 1, row)
}

function openDialog(name: string) {
  editCluster.value = name
  dialogOpen.value = true
}

function onSaved(name: string) {
  refreshOne(name)
}

onMounted(loadAll)

watch(
  () => clustersStore.clusterList,
  () => loadAll(),
  { deep: false },
)
</script>

<template>
  <div>
    <v-alert
      v-if="!loading && items.length === 0"
      type="info"
      class="mb-4"
    >
      {{ t('autosnapshot.clusters.noClusters') }}
    </v-alert>

    <v-card v-if="loading || items.length > 0">
      <v-card-text class="pb-0">
        <v-text-field
          v-model="search"
          :label="t('autosnapshot.clusters.filterCluster')"
          density="compact"
          variant="outlined"
          prepend-inner-icon="mdi-magnify"
          hide-details
          clearable
          class="mt-1"
          style="max-width: 360px"
        />
      </v-card-text>

      <v-data-table
        :headers="headers"
        :items="filteredItems"
        :loading="loading"
        item-value="ClusterName"
        hover
      >
      <template #item.ClusterName="{ item }">
        <div class="d-flex align-center ga-1">
          {{ item.ClusterName }}
          <v-btn
            v-if="!item.LoadError"
            v-tooltip="t('autosnapshot.clusters.edit')"
            icon="mdi-pencil"
            size="x-small"
            variant="text"
            density="comfortable"
            @click="openDialog(item.ClusterName)"
          />
        </div>
      </template>

      <template #item.Status="{ item }">
        <v-tooltip v-if="item.LoadError" :text="item.LoadError" location="top">
          <template #activator="{ props: tipProps }">
            <v-chip v-bind="tipProps" size="small" color="error" variant="tonal">
              {{ t('autosnapshot.clusters.loadError') }}
            </v-chip>
          </template>
        </v-tooltip>
        <v-chip
          v-else
          size="small"
          :color="item.HasOverride ? 'primary' : undefined"
          :variant="item.HasOverride ? 'tonal' : 'outlined'"
        >
          {{
            item.HasOverride
              ? t('autosnapshot.clusters.hasOverride')
              : t('autosnapshot.clusters.usesDefaults')
          }}
        </v-chip>
      </template>

      <template #item.Triggers="{ item }">
        <v-chip
          size="x-small"
          :color="item.ActivitySpikeEnabled ? 'success' : undefined"
          :variant="item.ActivitySpikeEnabled ? 'tonal' : 'outlined'"
          class="mr-1"
        >
          {{ t('autosnapshot.trigger.activity_spike') }}
        </v-chip>
        <v-chip
          size="x-small"
          :color="item.RoleChangeEnabled ? 'success' : undefined"
          :variant="item.RoleChangeEnabled ? 'tonal' : 'outlined'"
        >
          {{ t('autosnapshot.trigger.role_change') }}
        </v-chip>
      </template>

      </v-data-table>
    </v-card>

    <AutoSnapshotClusterDialog
      v-if="editCluster && dialogOpen"
      v-model="dialogOpen"
      :cluster-name="editCluster"
      @saved="onSaved"
    />
  </div>
</template>
