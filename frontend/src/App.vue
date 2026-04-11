<script setup lang="ts">

import { computed, ref, watch } from 'vue'
import { useThemeStore } from './stores/theme'
import { useAuthStore } from './stores/auth'
import { AuthInfoMode } from './api/models'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const themeStore = useThemeStore()
const authStore = useAuthStore()

import { useRoute } from 'vue-router'

const route = useRoute()

import ClusterHostDbSelector from './components/ClusterHostDbSelector.vue'
import LoginCard from './components/auth/LoginCard.vue'

function withQuery(base: string) {
  const cluster = route.params.clustername ?? '';
  const host = route.query.host ?? null;
  const db = route.query.db ?? null;

  const query: Record<string, string> = {};

  if (host) query.host = String(host);
  if (db) query.db = String(db);

  return {
    path: `/${base}/${cluster}`,
    query
  };
}

const mainLink = computed(() => withQuery("main"));
const connectionsLink = computed(() => withQuery("connections"));
const queriesLink = computed(() => withQuery("queries"));
const queryStatsLink = computed(() => withQuery("query-stats"));
const queryReportLink = computed(() => withQuery("query-report"));
const tablesLink = computed(() => withQuery("tables"));
const tableDescribeLink = computed(() => {
  const base = withQuery("table-describe");
  const schema = route.query.schema;
  const table = route.query.table;
  return {
    ...base,
    query: {
      ...base.query,
      ...(schema ? { schema: String(schema) } : {}),
      ...(table ? { table: String(table) } : {}),
    },
  };
});
const indexesLink = computed(() => withQuery("indexes"));
const indexesUsageLink = computed(() => withQuery("indexes-usage"));
const indexesProblemsLink = computed(() => withQuery("indexes-problems"));
const locksLink = computed(() => withQuery("locks"));
const progressLink = computed(() => withQuery("progress"));
const fkAnalysisLink = computed(() => withQuery("fk-analysis"));
const maintenanceLink = computed(() => withQuery("maintenance"));
const replicationLink = computed(() => withQuery("replication"));
const settingsLink = computed(() => withQuery("settings"));

const drawer = ref(true)

const openedGroups = ref<string[]>([])

function syncOpenedGroups() {
  const path = route.path
  if ((path.includes('/tables') || path.includes('/table-describe')) && !openedGroups.value.includes('tables')) {
    openedGroups.value.push('tables')
  }
  if (path.includes('/indexes') && !openedGroups.value.includes('indexes')) {
    openedGroups.value.push('indexes')
  }
}

syncOpenedGroups()
watch(() => route.path, syncOpenedGroups)

</script>

<template>
  <v-responsive class="border rounded">

    <v-app :theme="themeStore.theme">
      <template v-if="!authStore.initialized">
        <v-container class="fill-height d-flex align-center justify-center">
          <div class="text-center">
            <v-progress-circular indeterminate size="48" color="primary" class="mb-4" />
            <div class="text-h6 text-medium-emphasis">Dasha</div>
          </div>
        </v-container>
      </template>

      <template v-else-if="authStore.requiresLogin">
        <LoginCard />
      </template>

      <template v-else>
      <v-app-bar app color="primary" dark density="comfortable" elevation="2">
        <v-app-bar-nav-icon variant="text" @click.stop="drawer = !drawer" />
        <v-toolbar-title class="app-brand">
          <span class="app-brand-name">Dasha</span>
          <v-divider vertical class="mx-2 app-brand-divider" />
          <span class="app-brand-sub">PostgreSQL Dashboard</span>
        </v-toolbar-title>
        <cluster-host-db-selector class="ml-4 mr-2" />
        <template v-slot:append>
          <v-btn
            :icon="themeStore.icon()"
            v-tooltip="themeStore.currentTheme() === 'light' ? t('Switch to dark mode') : t('Switch to light mode')"
            slim
            @click="themeStore.toggleTheme()"
          ></v-btn>
          <template v-if="authStore.mode === AuthInfoMode.oidc">
            <template v-if="authStore.user">
              <v-menu location="bottom end" :close-on-content-click="false">
                <template #activator="{ props }">
                  <v-btn v-bind="props" icon variant="text" class="ml-1">
                    <v-icon>mdi-account-circle</v-icon>
                  </v-btn>
                </template>
                <v-card min-width="220">
                  <v-card-text class="text-center py-4">
                    <v-avatar color="primary" size="48" class="mb-2">
                      <span class="text-h6">{{ authStore.user.name?.charAt(0)?.toUpperCase() }}</span>
                    </v-avatar>
                    <div class="text-subtitle-1 font-weight-medium">{{ authStore.user.name }}</div>
                    <div class="text-caption text-medium-emphasis">{{ authStore.user.email }}</div>
                    <v-chip size="x-small" variant="tonal" class="mt-1">{{ authStore.user.role }}</v-chip>
                  </v-card-text>
                  <v-divider />
                  <v-card-actions>
                    <v-btn block variant="text" prepend-icon="mdi-logout" @click="authStore.logout">
                      {{ t('Logout') }}
                    </v-btn>
                  </v-card-actions>
                </v-card>
              </v-menu>
            </template>
            <v-btn v-else icon variant="text" class="ml-1" @click="authStore.doLoginRedirect">
              <v-icon>mdi-login</v-icon>
            </v-btn>
          </template>
        </template>
      </v-app-bar>

      <v-navigation-drawer v-model="drawer"
        :location="$vuetify.display.mobile ? 'bottom' : undefined"
        >
        <v-list nav :opened="openedGroups">
          <v-list-item :title="t('Home')"  prepend-icon="mdi-sigma" link :to="mainLink"></v-list-item>
          <v-list-item :title="t('Connections')" prepend-icon="mdi-connection" link :to="connectionsLink"></v-list-item>
          <v-list-item :title="t('Active Queries')" prepend-icon="mdi-database-clock-outline" link :to="queriesLink"></v-list-item>
          <v-list-item :title="t('Query Stats')" prepend-icon="mdi-chart-bar" link :to="queryStatsLink"></v-list-item>
          <v-list-item :title="t('Query Report')" prepend-icon="mdi-file-chart-outline" link :to="queryReportLink"></v-list-item>
          <v-list-group value="tables">
            <template #activator="{ props }">
              <v-list-item v-bind="props" :title="t('Tables')" prepend-icon="mdi-table"></v-list-item>
            </template>
            <v-list-item :title="t('tables.menuOverview')" link :to="tablesLink"></v-list-item>
            <v-list-item :title="t('tables.menuDescribe')" link :to="tableDescribeLink"></v-list-item>
          </v-list-group>
          <v-list-group value="indexes">
            <template #activator="{ props }">
              <v-list-item v-bind="props" :title="t('Indexes')" prepend-icon="mdi-family-tree"></v-list-item>
            </template>
            <v-list-item :title="t('indexes.menuOverview')" link :to="indexesLink"></v-list-item>
            <v-list-item :title="t('indexes.menuUsage')" link :to="indexesUsageLink"></v-list-item>
            <v-list-item :title="t('indexes.menuProblems')" link :to="indexesProblemsLink"></v-list-item>
          </v-list-group>
          <v-list-item :title="t('Locks')" prepend-icon="mdi-lock-outline" link :to="locksLink"></v-list-item>
          <v-list-item :title="t('Operation progress')" prepend-icon="mdi-progress-question" link :to="progressLink"></v-list-item>
          <v-list-item :title="t('FK Analysis')" prepend-icon="mdi-relation-many-to-many" link :to="fkAnalysisLink"></v-list-item>
          <v-list-item :title="t('Replication')" prepend-icon="mdi-database-sync-outline" link :to="replicationLink"></v-list-item>
          <v-list-item :title="t('Maintenance')" prepend-icon="mdi-wrench-outline" link :to="maintenanceLink"></v-list-item>
          <v-list-item :title="t('Settings')" prepend-icon="mdi-database-settings-outline" link :to="settingsLink"></v-list-item>
        </v-list>
      </v-navigation-drawer>

      <v-main>
        <v-container>
          <router-view v-slot="{ Component }">
              <component :is="Component" />
          </router-view>
        </v-container>
      </v-main>
      </template>
    </v-app>
  </v-responsive>
</template>

<style scoped>
.app-brand {
  display: flex;
  align-items: center;
  gap: 0;
}

.app-brand-name {
  font-size: 1.25rem;
  font-weight: 700;
  letter-spacing: 0.5px;
}

.app-brand-divider {
  opacity: 0.4;
  height: 20px;
}

.app-brand-sub {
  font-size: 1.25rem;
  font-weight: 300;
  opacity: 0.7;
}

</style>

