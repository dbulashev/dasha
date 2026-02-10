import { ref, watch, type Ref } from 'vue'

export function useDebouncedRef<T>(source: Ref<T>, delay: number): Ref<T> {
  const debounced = ref(source.value) as Ref<T>
  let timer: ReturnType<typeof setTimeout> | null = null

  watch(source, (val) => {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => {
      debounced.value = val
    }, delay)
  })

  return debounced
}
