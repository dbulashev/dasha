<script setup lang="ts">

import { computed, ref } from 'vue'
import { useThemeStore } from './stores/theme'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const themeStore = useThemeStore()

import { useRoute } from 'vue-router'

const route = useRoute()

import ClusterHostDbSelector from './components/ClusterHostDbSelector.vue'

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
const indexesLink = computed(() => withQuery("indexes"));
const indexesUsageLink = computed(() => withQuery("indexes-usage"));
const indexesProblemsLink = computed(() => withQuery("indexes-problems"));
const locksLink = computed(() => withQuery("locks"));
const progressLink = computed(() => withQuery("progress"));
const fkAnalysisLink = computed(() => withQuery("fk-analysis"));
const maintenanceLink = computed(() => withQuery("maintenance"));
const settingsLink = computed(() => withQuery("settings"));

const drawer = ref(true)

</script>

<template>
  <v-responsive class="border rounded">
    
    <v-app :theme="themeStore.theme">
      <v-app-bar app color="primary" dark density="comfortable">
        <template v-slot:prepend>
            <v-app-bar-nav-icon variant="text" @click.stop="drawer = !drawer"></v-app-bar-nav-icon>
        </template>
        <v-toolbar-title>PostgreSQL Dashboard</v-toolbar-title>
         <cluster-host-db-selector class="ml-5"></cluster-host-db-selector>
        <template v-slot:append>
          <v-btn
            :icon="themeStore.icon()"
            v-tooltip="themeStore.currentTheme() === 'light' ? t('Switch to dark mode') : t('Switch to light mode')"
            slim
            @click="themeStore.toggleTheme()"
          ></v-btn>
        </template>
      </v-app-bar>

      <v-navigation-drawer v-model="drawer"
        :location="$vuetify.display.mobile ? 'bottom' : undefined"
        >
        <v-list nav>
          <v-list-item :title="t('Home')"  prepend-icon="mdi-sigma" link :to="mainLink"></v-list-item>
          <v-list-item :title="t('Connections')" prepend-icon="mdi-connection" link :to="connectionsLink"></v-list-item>
          <v-list-item :title="t('Active Queries')" prepend-icon="mdi-database-clock-outline" link :to="queriesLink"></v-list-item>
          <v-list-item :title="t('Query Stats')" prepend-icon="mdi-chart-bar" link :to="queryStatsLink"></v-list-item>
          <v-list-item :title="t('Query Report')" prepend-icon="mdi-file-chart-outline" link :to="queryReportLink"></v-list-item>
          <v-list-item :title="t('Tables')" prepend-icon="mdi-table" link :to="tablesLink"></v-list-item>
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
    </v-app>
  </v-responsive>
</template>

