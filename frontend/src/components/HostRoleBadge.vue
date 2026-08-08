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
const version = computed(() => instanceInfoStore.known(cluster.value, host.value)?.Version)
// While the info is still on its way the badge shows a placeholder: calling the
// role unknown before the answer arrives makes it blink on every host switch.
const pending = computed(() => instanceInfoStore.pending(cluster.value, host.value))

const appearance = computed(() => {
  switch (role.value) {
    case 'primary':
      return {
        color: 'success',
        variant: 'flat' as const,
        icon: 'mdi-crown',
        label: t('hostRole.primary'),
        hint: t('hostRole.primaryHint'),
      }
    case 'replica':
      return {
        color: 'info',
        variant: 'flat' as const,
        icon: 'mdi-content-copy',
        label: t('hostRole.replica'),
        hint: t('hostRole.replicaHint'),
      }
    default:
      return {
        color: undefined,
        variant: 'tonal' as const,
        icon: 'mdi-help-circle-outline',
        label: t('hostRole.unknown'),
        hint: t('hostRole.unknownHint'),
      }
  }
})
</script>

<template>
  <div v-if="host" class="host-role">
    <div class="pa-2">
      <v-skeleton-loader v-if="pending" type="chip" class="host-role-skeleton" />
      <v-tooltip v-else :text="appearance.hint" location="bottom">
        <template #activator="{ props }">
          <v-chip
            v-bind="props"
            :color="appearance.color"
            :variant="appearance.variant"
            size="small"
            label
            class="host-role-chip"
          >
            <v-icon start :icon="appearance.icon" />
            {{ appearance.label }}
            <span v-if="version" class="host-role-version ml-1">· {{ version }}</span>
          </v-chip>
        </template>
      </v-tooltip>
    </div>
    <v-divider />
  </div>
</template>

<style scoped>
/* Pinned to the top of the drawer so the menu scrolls underneath it. */
.host-role {
  position: sticky;
  top: 0;
  z-index: 2;
  background: rgb(var(--v-theme-surface));
}

.host-role-chip {
  width: 100%;
}

/* Same footprint as the chip it stands in for, so the menu does not shift. */
.host-role-skeleton {
  background: transparent;
}

.host-role-skeleton :deep(.v-skeleton-loader__chip) {
  width: 100%;
  margin: 0;
}

.host-role-version {
  opacity: 0.8;
}
</style>
