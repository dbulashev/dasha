import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { DEFAULT_PAGE_SIZE, LARGE_PAGE_FACTOR } from '@/constants/pagination'

// 'local' renders timestamps in the viewer's own zone; any other value is an IANA
// zone id ('UTC', 'Europe/Moscow', …) passed straight to Intl. A fixed zone is what
// a DBA usually wants when correlating the UI with server logs and pg_stat_activity,
// which are kept in the server's zone rather than the reader's.
export type TimeZoneSetting = 'local' | string

export const usePrefsStore = defineStore(
  'prefs',
  () => {
    const timezone = ref<TimeZoneSetting>('local')
    const pageSize = ref<number>(DEFAULT_PAGE_SIZE)

    // Compact-row tables scale off the same setting, keeping the 2:1 ratio the
    // hardcoded constants used to have.
    const largePageSize = computed(() => pageSize.value * LARGE_PAGE_FACTOR)

    return { timezone, pageSize, largePageSize }
  },
  {
    persist: {
      storage: localStorage,
    },
  },
)
