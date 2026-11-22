import { computed, type Ref } from 'vue'

import { AuthInfoMode, type Cluster } from '@/api/models'
import { useAuthStore } from '@/stores/auth'
import { useAutosnapshotStatusStore } from '@/stores/autosnapshotStatus'
import { useInstanceInfoStore } from '@/stores/instanceInfo'
import { useSnapshotsStatusStore } from '@/stores/snapshotsStatus'
import { defaultHost } from '@/utils/clusters'
import {
  isSectionVisible,
  sectionBySlug,
  type SectionDef,
  type VisibilityFacts,
} from '@/utils/palette/sections'

export interface VisibilityTarget {
  cluster: Cluster
  /** read only when current; another cluster is judged by its default host (§2.2) */
  host: string | null
  /** the place the user is on now, as opposed to a cluster the palette offers */
  current: boolean
}

/**
 * Which sections exist for a cluster and host. The drawer menu and the command
 * palette read the same rules; `current` is the place in the URL, for the menu.
 */
export function useSectionVisibility(current: { cluster: Ref<string>; host: Ref<string> }) {
  const authStore = useAuthStore()
  const instanceInfoStore = useInstanceInfoStore()
  const snapshotsStatusStore = useSnapshotsStatusStore()
  const autosnapshotStatusStore = useAutosnapshotStatusStore()

  const isAdmin = computed(
    () => authStore.mode === AuthInfoMode.none || authStore.user?.role === 'admin',
  )
  const autosnapshotVisible = computed(
    () =>
      autosnapshotStatusStore.available &&
      (autosnapshotStatusStore.enabled || isAdmin.value),
  )

  function facts(cluster: string, host: string, isCurrent: boolean, logs: boolean): VisibilityFacts {
    return {
      current: isCurrent,
      versionNum: instanceInfoStore.known(cluster, host)?.VersionNum ?? null,
      replica: instanceInfoStore.isReplica(cluster, host),
      snapshots: snapshotsStatusStore.available,
      autosnapshot: autosnapshotVisible.value,
      logs,
    }
  }

  // Facts are gathered once per target and only for a rule that needs them:
  // the search asks about every section of every cluster on each keystroke.
  function visibilityFor(target: VisibilityTarget): (section: SectionDef) => boolean {
    let known: VisibilityFacts | null = null
    return section => {
      if (section.rule === 'always') return true
      known ??= facts(
        target.cluster.name ?? '',
        (target.current ? target.host : defaultHost(target.cluster)) ?? '',
        target.current,
        target.cluster.supports_logs === true,
      )
      return isSectionVisible(section, known)
    }
  }

  function visible(section: SectionDef, target: VisibilityTarget): boolean {
    return visibilityFor(target)(section)
  }

  /** The search's view of visibility: by cluster name, the current one judged by the URL host. */
  function visibleByName(clusters: readonly Cluster[]): (cluster: string) => (section: SectionDef) => boolean {
    const byName = new Map(clusters.map(c => [c.name ?? '', c]))
    return name => {
      const isCurrent = name === current.cluster.value
      return visibilityFor({
        cluster: byName.get(name) ?? { name },
        host: isCurrent ? current.host.value : null,
        current: isCurrent,
      })
    }
  }

  // The menu shows Logs when any cluster has them, not per cluster: that rule
  // stays in App.vue, and logs do not enter the facts here.
  function menuVisible(slug: string): boolean {
    return isSectionVisible(
      sectionBySlug(slug)!,
      facts(current.cluster.value, current.host.value, true, false),
    )
  }

  const isReplica = computed(() =>
    instanceInfoStore.isReplica(current.cluster.value, current.host.value),
  )

  return { isAdmin, autosnapshotVisible, isReplica, menuVisible, visible, visibilityFor, visibleByName }
}
