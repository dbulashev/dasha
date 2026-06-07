<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { QueryBlocked } from '@/api/models/index'
import { fmtMs as fmtMsUtil } from '@/utils/format'
import '@/assets/sql-highlight.css'

const props = defineProps<{ items: QueryBlocked[]; loading?: boolean }>()

const { t } = useI18n()

function fmtDuration(ms: number | null | undefined, fallback: string): string {
  return ms == null ? fallback : fmtMsUtil(ms, t)
}

interface LockTreeNode {
  pid: number
  user: string
  query: string
  duration: string
  mode: string
  state: string
  blocked: { pid: number; user: string; query: string; duration: string; mode: string; lockedItem: string }[]
}

// Group blocked/blocking pairs by blocking PID into a one-level tree.
const lockTree = computed<LockTreeNode[]>(() => {
  const map = new Map<number, LockTreeNode>()
  for (const item of props.items) {
    let node = map.get(item.BlockingPid)
    if (!node) {
      node = {
        pid: item.BlockingPid,
        user: item.BlockingUser,
        query: item.CurrentOrRecentQueryInBlockingProcess,
        duration: fmtDuration(item.BlockingDurationMs, item.BlockingDuration),
        mode: item.BlockingMode,
        state: item.StateOfBlockingProcess,
        blocked: [],
      }
      map.set(item.BlockingPid, node)
    }
    node.blocked.push({
      pid: item.BlockedPid,
      user: item.BlockedUser,
      query: item.BlockedQuery,
      duration: fmtDuration(item.BlockedDurationMs, item.BlockedDuration),
      mode: item.BlockedMode,
      lockedItem: item.LockedItem,
    })
  }
  return Array.from(map.values())
})
</script>

<template>
  <v-progress-linear v-if="loading" indeterminate />
  <div v-else-if="lockTree.length === 0" class="text-medium-emphasis">
    {{ t('locks.noActiveLocks') }}
  </div>
  <div v-else>
    <v-card
      v-for="node in lockTree"
      :key="node.pid"
      variant="outlined"
      class="mb-3"
    >
      <v-card-title class="text-subtitle-1 d-flex align-center ga-2">
        <v-icon color="error" size="small">mdi-lock</v-icon>
        <span>PID {{ node.pid }} ({{ node.user }})</span>
        <v-chip size="small" color="warning" variant="tonal">{{ node.state }}</v-chip>
        <v-chip size="small" variant="tonal">{{ node.mode }}</v-chip>
        <v-chip size="small" variant="tonal">{{ node.duration }}</v-chip>
      </v-card-title>
      <v-card-subtitle class="text-wrap font-weight-regular sql-code text-mono text-caption">
        {{ node.query }}
      </v-card-subtitle>
      <v-card-text>
        <div class="text-caption text-medium-emphasis mb-1">
          {{ t('locks.blockedProcesses', { count: node.blocked.length }) }}:
        </div>
        <v-list density="compact" lines="two">
          <v-list-item
            v-for="b in node.blocked"
            :key="b.pid"
            prepend-icon="mdi-arrow-right-bold"
          >
            <v-list-item-title class="d-flex align-center ga-2">
              <span>PID {{ b.pid }} ({{ b.user }})</span>
              <v-chip size="x-small" variant="tonal">{{ b.mode }}</v-chip>
              <v-chip size="x-small" variant="tonal">{{ b.duration }}</v-chip>
              <v-chip size="x-small" color="info" variant="tonal">{{ b.lockedItem }}</v-chip>
            </v-list-item-title>
            <v-list-item-subtitle class="sql-code text-mono text-caption">
              {{ b.query }}
            </v-list-item-subtitle>
          </v-list-item>
        </v-list>
      </v-card-text>
    </v-card>
  </div>
</template>
