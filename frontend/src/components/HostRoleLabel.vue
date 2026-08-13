<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import { useInstanceInfoStore } from '@/stores/instanceInfo'

const { t } = useI18n()
const route = useRoute()
const instanceInfoStore = useInstanceInfoStore()

const cluster = computed(() => String(route.params.clustername ?? ''))
const host = computed(() => String(route.query.host ?? ''))

const role = computed(() => instanceInfoStore.role(cluster.value, host.value))
// server_version may carry a packager suffix ("17.2 (Debian ...)"): keep the number only.
const version = computed(
  () => instanceInfoStore.known(cluster.value, host.value)?.Version?.split(' ')[0] ?? '',
)
// Nothing is shown until the answer arrives: calling the role unknown before
// that makes the title blink on every host switch.
const pending = computed(() => instanceInfoStore.pending(cluster.value, host.value))

const label = computed(() => {
  switch (role.value) {
    case 'primary':
      return t('hostRole.primary')
    case 'replica':
      return t('hostRole.replica')
    default:
      return t('hostRole.unknown')
  }
})
</script>

<template>
  <template v-if="host && !pending">
    <v-divider vertical class="mx-2 host-role-divider" />
    <span class="host-role">
      {{ label }}<template v-if="version"> · {{ version }}</template>
    </span>
  </template>
</template>

<style scoped>
.host-role-divider {
  opacity: 0.4;
  height: 20px;
}

/* Same type as the brand subtitle, one step further back in the hierarchy. */
.host-role {
  font-size: 1.25rem;
  font-weight: 300;
  opacity: 0.45;
  white-space: nowrap;
}
</style>
