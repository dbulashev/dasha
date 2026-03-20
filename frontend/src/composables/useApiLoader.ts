import { ref, watch, type Ref, type WatchSource } from 'vue'
import { assertOk } from '@/utils/api'

interface UseApiLoaderOptions<T> {
  deps: WatchSource[]
  guard: () => boolean
  onError: (msg: string) => void
  defaultValue?: T
}

interface UseApiLoaderReturn<T> {
  items: Ref<T>
  loading: Ref<boolean>
  load: () => Promise<void>
}

export function useApiLoader<T = unknown[]>(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  fetcher: () => Promise<{ data: any; status: number }>,
  options: UseApiLoaderOptions<T>,
): UseApiLoaderReturn<T> {
  const items = ref(options.defaultValue ?? ([] as unknown as T)) as Ref<T>
  const loading = ref(false)

  async function load() {
    if (!options.guard()) return
    loading.value = true
    try {
      const response = await fetcher()
      items.value = assertOk(response) ?? (options.defaultValue ?? ([] as unknown as T))
    } catch (err) {
      options.onError(String(err))
      items.value = options.defaultValue ?? ([] as unknown as T)
    } finally {
      loading.value = false
    }
  }

  watch(options.deps, () => load(), { immediate: true })

  return { items, loading, load }
}

interface UsePaginatedApiLoaderOptions<T> {
  pageSize: number
  deps: WatchSource[]
  guard: () => boolean
  onError: (msg: string) => void
  defaultValue?: T[]
}

interface UsePaginatedApiLoaderReturn<T> {
  items: Ref<T[]>
  loading: Ref<boolean>
  page: Ref<number>
  hasMore: Ref<boolean>
  load: (p?: number) => Promise<void>
}

export function usePaginatedApiLoader<T>(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  fetcher: (limit: number, offset: number) => Promise<{ data: any; status: number }>,
  options: UsePaginatedApiLoaderOptions<T>,
): UsePaginatedApiLoaderReturn<T> {
  const items = ref(options.defaultValue ?? []) as Ref<T[]>
  const loading = ref(false)
  const page = ref(1)
  const hasMore = ref(true)

  async function load(p = 1) {
    if (!options.guard()) return
    loading.value = true
    try {
      const offset = (p - 1) * options.pageSize
      const response = await fetcher(options.pageSize, offset)
      const data = assertOk(response) ?? []
      items.value = data
      page.value = p
      hasMore.value = data.length >= options.pageSize
    } catch (err) {
      options.onError(String(err))
      items.value = []
    } finally {
      loading.value = false
    }
  }

  watch(options.deps, () => load(), { immediate: true })

  return { items, loading, page, hasMore, load }
}
