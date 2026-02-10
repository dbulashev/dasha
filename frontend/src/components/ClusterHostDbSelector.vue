<template>
  <div class="d-flex align-center" style="gap: 10px">

    <!-- Cluster -->
    <v-autocomplete
      v-model="selectedCluster"
      :items="clusterNames"
      :loading="loading"
      :label="t('Database Cluster')"
      density="compact"
      hide-details
      style="min-width: 170px"
    />

    <!-- Host -->
    <v-autocomplete
      v-model="selectedHost"
      :items="hostOptions"
      :disabled="!selectedCluster"
      :loading="loading"
      :label="t('Database Host')"
      density="compact"
      hide-details
      style="min-width: 170px"
    />

    <!-- Database -->
    <v-autocomplete
      v-model="selectedDb"
      :items="dbOptions"
      :disabled="!selectedCluster"
      :loading="loading"
      :label="t('Database')"
      density="compact"
      hide-details
      style="min-width: 170px"
    />

  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch, computed, nextTick } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { useClustersStore } from "@/stores/clusters";
import { getClusters } from "@/api/gen/default/default";

const { t } = useI18n();
const clusterStore = useClustersStore();
const route = useRoute();
const router = useRouter();

// STATE
const loading = ref(false);
const error = ref<string | null>(null);
// True until first syncStateFromUrl completes — suppresses all watchers during init
const initialized = ref(false);

// SELECTED VALUES
const selectedCluster = ref<string | null>(null);
const selectedHost = ref<string | null>(null);
const selectedDb = ref<string | null>(null);

// Flag to prevent circular URL ↔ state updates
const isSyncing = ref(false);

// -----------------------------
// OPTIONS
// -----------------------------
const clusterNames = computed(() =>
  (clusterStore.clusterList?.map(c => c.name!) ?? []).sort(),
);

const hostOptions = computed(() => {
  const c = clusterStore.clusterList?.find(c => c.name === selectedCluster.value);
  return (c?.instances?.map(i => i.host_name!).filter(Boolean) ?? []).sort();
});

const dbOptions = computed(() => {
  const c = clusterStore.clusterList?.find(c => c.name === selectedCluster.value);
  return [...(c?.databases ?? [])].sort();
});

// -----------------------------
// HELPERS
// -----------------------------

// Resolve host: use provided value if valid, otherwise fall back to first available
function resolveHost(desired: string | null, hosts: string[]): string | null {
  if (desired && hosts.includes(desired)) return desired;
  return hosts.length > 0 ? hosts[0]! : null;
}

// Resolve db: use provided value if valid, otherwise fall back to first available
function resolveDb(desired: string | null, dbs: string[]): string | null {
  if (desired && dbs.includes(desired)) return desired;
  return dbs.length > 0 ? dbs[0]! : null;
}

// Push current selection to URL, preserving the current route name and path
function pushToUrl() {
  if (!selectedCluster.value) return;

  const targetHost = selectedHost.value;
  const targetDb = selectedDb.value;

  // Don't push if host is null when hosts are available (transient state)
  if (!targetHost && hostOptions.value.length > 0) return;

  router.replace({
    name: route.name!,
    params: { clustername: selectedCluster.value },
    query: {
      ...(targetHost ? { host: targetHost } : {}),
      ...(targetDb ? { db: targetDb } : {}),
    },
  });
}

// -----------------------------
// SYNC URL → STATE (one-directional: URL wins)
// -----------------------------
function syncStateFromUrl() {
  if (!clusterStore.clusterList?.length) return;

  isSyncing.value = true;

  const urlCluster = route.params.clustername ? String(route.params.clustername) : null;
  const urlHost = route.query.host ? String(route.query.host) : null;
  const urlDb = route.query.db ? String(route.query.db) : null;

  const names = clusterNames.value;

  // Resolve cluster
  if (urlCluster && names.includes(urlCluster)) {
    selectedCluster.value = urlCluster;
  } else if (names.length > 0) {
    selectedCluster.value = names[0]!;
  }

  // Resolve host and db based on selected cluster
  if (selectedCluster.value) {
    const cluster = clusterStore.clusterList!.find(c => c.name === selectedCluster.value);
    if (cluster) {
      const hosts = cluster.instances?.map(i => i.host_name!).filter(Boolean) ?? [];
      const dbs = cluster.databases ?? [];

      selectedHost.value = resolveHost(urlHost, hosts);
      selectedDb.value = resolveDb(urlDb, dbs);
    }
  }

  isSyncing.value = false;
}

// -----------------------------
// LOAD CLUSTERS
// -----------------------------
async function loadClusters() {
  // Wait for the router to finish initial navigation so route.name/params are correct
  await router.isReady();

  // Use cached data if still valid
  if (clusterStore.isCacheValid && clusterStore.clusterList?.length) {
    syncStateFromUrl();
    await nextTick();
    initialized.value = true;
    pushToUrl();
    return;
  }

  loading.value = true;
  try {
    const res = await getClusters();
    clusterStore.setClusters(res.data);
    syncStateFromUrl();
    await nextTick();
    initialized.value = true;
    pushToUrl();
  } catch (err: unknown) {
    error.value = err instanceof Error ? err.message : String(err);
    clusterStore.invalidateCache();
  } finally {
    loading.value = false;
  }
}

onMounted(loadClusters);

// -----------------------------
// WATCH: URL → STATE (external navigation, e.g. nav drawer links)
// -----------------------------
watch(
  () => [route.params.clustername, route.query.host, route.query.db],
  () => {
    if (!initialized.value || isSyncing.value) return;
    syncStateFromUrl();
  },
  { deep: true }
);

// -----------------------------
// WATCH: CLUSTER CHANGE by user → reset host/db to valid defaults
// -----------------------------
watch(selectedCluster, (newCluster, oldCluster) => {
  if (!initialized.value || isSyncing.value || !newCluster) return;
  if (newCluster === oldCluster) return;

  const cluster = clusterStore.clusterList?.find(c => c.name === newCluster);
  if (!cluster) return;

  const hosts = cluster.instances?.map(i => i.host_name!).filter(Boolean) ?? [];
  const dbs = cluster.databases ?? [];

  // When user changes cluster, reset host and db to first available values
  isSyncing.value = true;
  selectedHost.value = resolveHost(null, hosts);
  selectedDb.value = resolveDb(null, dbs);
  isSyncing.value = false;

  pushToUrl();
});

// -----------------------------
// WATCH: HOST or DB CHANGE by user → update URL
// -----------------------------
watch([selectedHost, selectedDb], () => {
  if (!initialized.value || isSyncing.value) return;
  if (!selectedCluster.value) return;
  pushToUrl();
});
</script>
