import { computed, watch } from 'vue'

import { useClusterInfo } from '@/composables/useClusterInfo'
import { useInstanceInfoStore } from '@/stores/instanceInfo'

// Version gating for the selected host. `pending` is false once the fetch has
// failed, so a section can fall back to the endpoint's own 501 instead of
// waiting forever.
export function useServerVersion() {
  const { clusterName, hostName } = useClusterInfo()
  const instanceInfo = useInstanceInfoStore()

  watch(
    [clusterName, hostName],
    ([cluster, host]) => {
      if (cluster && host) instanceInfo.ensure(cluster, host)
    },
    { immediate: true },
  )

  const info = computed(() => instanceInfo.known(clusterName.value ?? '', hostName.value ?? ''))
  const versionNum = computed(() => info.value?.VersionNum ?? null)
  const pending = computed(() => instanceInfo.pending(clusterName.value ?? '', hostName.value ?? ''))

  // False while the version is unknown.
  function atLeast(num: number): boolean {
    return versionNum.value !== null && versionNum.value >= num
  }

  // True only for a version known to be older.
  function below(num: number): boolean {
    return versionNum.value !== null && versionNum.value < num
  }

  return { info, versionNum, pending, atLeast, below }
}
