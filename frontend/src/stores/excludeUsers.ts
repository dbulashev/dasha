import { defineStore } from 'pinia'

export const useExcludeUsersStore = defineStore('excludeUsers', {
  state: () => ({
    byInstance: {} as Record<string, string[]>,
  }),
  actions: {
    getExcludeUsers(cluster: string): string[] {
      return this.byInstance[cluster] ?? []
    },
    setExcludeUsers(cluster: string, users: string[]) {
      this.byInstance[cluster] = users
    },
  },
  persist: {
    storage: localStorage,
  },
})
