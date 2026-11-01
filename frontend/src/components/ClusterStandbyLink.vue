<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { useClusterInfo } from '@/composables/useClusterInfo'
import { useClustersStore } from '@/stores/clusters'
import { primaryOf, standbysOf } from '@/utils/clusters'

const { t } = useI18n()
const route = useRoute()
const clusterStore = useClustersStore()
const { clusterName } = useClusterInfo()

const primary = computed(() => primaryOf(clusterStore.clusterList, clusterName.value))
const standbys = computed(() => standbysOf(clusterStore.clusterList, clusterName.value))

// Host and database belong to the current cluster: the selector picks defaults for the new one.
function linkTo(name: string) {
  return { name: route.name!, params: { clustername: name } }
}
</script>

<template>
  <div v-if="primary || standbys.length" class="d-flex flex-wrap align-center ga-2">
    <template v-if="primary">
      <v-icon icon="mdi-source-branch" size="small" />
      <span>{{ t('replication.standbyOf') }}</span>
      <router-link :to="linkTo(primary)">{{ primary }}</router-link>
    </template>
    <template v-if="standbys.length">
      <v-icon icon="mdi-source-fork" size="small" />
      <span>{{ t('replication.standbyClusters') }}</span>
      <template v-for="(name, i) in standbys" :key="name">
        <router-link :to="linkTo(name)">{{ name }}</router-link><span v-if="i < standbys.length - 1">,</span>
      </template>
    </template>
  </div>
</template>
