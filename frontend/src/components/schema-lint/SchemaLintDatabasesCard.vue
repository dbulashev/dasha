<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { getSchemaLintSummary } from '@/api/gen/default/default'
import type { SchemaLintDatabaseSummary } from '@/api/models/index'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useViewError } from '@/composables/useViewError'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { LEVEL_COLOR } from './levels'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { clusterName, hostName, databaseName } = useClusterInfo()
const { onError } = useViewError()

const items = ref<SchemaLintDatabaseSummary[]>([])
const loading = ref(false)
const expanded = ref(false)

// Guards against out-of-order responses: the sweep walks every database and may
// take minutes, so a reply for the instance the user has already left must not
// land on the one now on screen.
let reqId = 0

// The sweep visits every database of the instance, so it is only run when the
// user asks for the overview — not on every visit to the page.
async function load() {
  if (!clusterName.value || !hostName.value) return

  const myId = ++reqId
  loading.value = true
  try {
    const res = await getSchemaLintSummary({
      cluster_name: clusterName.value,
      instance: hostName.value,
    })
    if (myId !== reqId) return // superseded — leave state to the newer load

    items.value = assertOk<SchemaLintDatabaseSummary[]>(res) ?? []
  } catch (err) {
    if (myId !== reqId) return

    onError(getErrorMessage(err), err)
    items.value = []
  } finally {
    if (myId === reqId) loading.value = false
  }
}

watch([clusterName, hostName], () => {
  reqId++ // whatever is in flight describes the previous instance
  items.value = []
  if (expanded.value) load()
})

watch(expanded, (open) => {
  if (open && !items.value.length) load()
})

const headers = computed(() => [
  { title: t('header.database'), key: 'database' },
  { title: t('schemaLint.levels.error'), key: 'error', width: 110 },
  { title: t('schemaLint.levels.warning'), key: 'warning', width: 140 },
  { title: t('schemaLint.levels.notice'), key: 'notice', width: 130 },
  { title: t('schemaLint.databases.skipped'), key: 'skipped', width: 130 },
])

function openDatabase(db: string) {
  router.push({ path: route.path, query: { ...route.query, db } })
}
</script>

<template>
  <v-card variant="outlined" class="mb-4">
    <v-card-title class="d-flex align-center ga-2">
      <v-icon start icon="mdi-database-search-outline" size="small" />
      <span class="text-subtitle-1">{{ t('schemaLint.databases.title') }}</span>
      <v-spacer />
      <v-btn
        variant="text"
        size="small"
        :append-icon="expanded ? 'mdi-chevron-up' : 'mdi-chevron-down'"
        @click="expanded = !expanded"
      >
        {{ expanded ? t('schemaLint.databases.hide') : t('schemaLint.databases.show') }}
      </v-btn>
    </v-card-title>

    <v-expand-transition>
      <div v-show="expanded">
        <v-card-text>
          <div class="text-caption text-medium-emphasis mb-2">
            {{ t('schemaLint.databases.hint') }}
          </div>

          <v-data-table
            :headers="headers"
            :items="items"
            :loading="loading"
            item-value="database"
            density="compact"
            hide-default-footer
            :items-per-page="-1"
          >
            <template #item.database="{ item }">
              <a
                href="#"
                class="text-decoration-none"
                :class="{ 'font-weight-bold': item.database === databaseName }"
                @click.prevent="openDatabase(item.database)"
              >{{ item.database }}</a>
              <v-chip v-if="item.failed" size="x-small" color="warning" variant="tonal" class="ml-2">
                {{ t('schemaLint.databases.failed') }}
              </v-chip>
            </template>

            <template v-for="level in ['error', 'warning', 'notice']" :key="level" #[`item.${level}`]="{ item }">
              <span v-if="item.failed" class="text-medium-emphasis">—</span>
              <v-chip
                v-else-if="item[level as 'error' | 'warning' | 'notice']"
                size="small"
                variant="tonal"
                :color="LEVEL_COLOR[level]"
              >
                {{ item[level as 'error' | 'warning' | 'notice'] }}
              </v-chip>
              <span v-else class="text-medium-emphasis">0</span>
            </template>

            <!-- Without this column zero findings would read as a clean database
                 even when nothing could be checked there. -->
            <template #item.skipped="{ item }">
              <span v-if="item.failed" class="text-medium-emphasis">—</span>
              <v-tooltip v-else-if="item.skipped" :text="t('schemaLint.databases.skippedHint')" location="bottom">
                <template #activator="{ props }">
                  <v-chip v-bind="props" size="small" variant="tonal" color="warning" prepend-icon="mdi-help-rhombus-outline">
                    {{ item.skipped }}
                  </v-chip>
                </template>
              </v-tooltip>
              <span v-else class="text-medium-emphasis">0</span>
            </template>

            <template #no-data>
              <div class="text-center py-3 text-medium-emphasis">
                {{ loading ? t('schemaLint.databases.loading') : t('schemaLint.databases.empty') }}
              </div>
            </template>
          </v-data-table>
        </v-card-text>
      </div>
    </v-expand-transition>
  </v-card>
</template>
