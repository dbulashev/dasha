import { ref, toValue, watch, type MaybeRefOrGetter, type Ref, type WatchSource } from 'vue'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'

interface UseApiLoaderOptions<T> {
  deps: WatchSource[]
  guard: () => boolean
  onError: (msg: string, err?: unknown) => void
  defaultValue?: T
}

interface UseApiLoaderReturn<T> {
  items: Ref<T>
  loading: Ref<boolean>
  load: () => Promise<void>
}

 
export function useApiLoader<T = unknown[]>(
  fetcher: () => Promise<{ data: unknown; status: number }>,
  options: UseApiLoaderOptions<T>,
): UseApiLoaderReturn<T> {
  const items = ref(options.defaultValue ?? ([] as unknown as T)) as Ref<T>
  const loading = ref(false)

  async function load() {
    if (!options.guard()) return
    loading.value = true
    try {
      const response = await fetcher()
      items.value = (assertOk(response) as T) ?? (options.defaultValue ?? ([] as unknown as T))
    } catch (err) {
      options.onError(getErrorMessage(err), err)
      items.value = options.defaultValue ?? ([] as unknown as T)
    } finally {
      loading.value = false
    }
  }

  watch(options.deps, () => load(), { immediate: true })

  return { items, loading, load }
}

interface UsePaginatedApiLoaderOptions<T> {
  // A getter/ref so the user-configured page size takes effect immediately: the
  // size is part of the request (limit/offset), so a change has to refetch.
  pageSize: MaybeRefOrGetter<number>
  deps: WatchSource[]
  guard: () => boolean
  onError: (msg: string, err?: unknown) => void
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
  fetcher: (limit: number, offset: number) => Promise<{ data: unknown; status: number }>,
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
      const pageSize = toValue(options.pageSize)
      const offset = (p - 1) * pageSize
      const response = await fetcher(pageSize, offset)
      const data = (assertOk(response) as T[]) ?? []
      items.value = data
      page.value = p
      hasMore.value = data.length >= pageSize
    } catch (err) {
      options.onError(getErrorMessage(err), err)
      items.value = []
    } finally {
      loading.value = false
    }
  }

  // Resizing the page invalidates the current offset, so reload from page 1.
  watch([...options.deps, () => toValue(options.pageSize)], () => load(), { immediate: true })

  return { items, loading, page, hasMore, load }
}
