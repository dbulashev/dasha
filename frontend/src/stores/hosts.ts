import { defineStore } from 'pinia'

import type { ClusterInstance } from '@/api/models'

export const useClusterInstances = defineStore('clusters_instances', {
  state: () => {
    return {
      Instances: null as ClusterInstance[] | null,
    }
  },
})
