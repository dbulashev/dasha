import { inject, ref, type InjectionKey, type Ref } from 'vue'
import { ApiError } from '@/utils/api'

export interface GlobalError {
  code: number
  message: string
}

export const errorKey = Symbol('viewError') as InjectionKey<Ref<GlobalError | null>>

export function useViewError() {
  const globalError = inject(errorKey, ref<GlobalError | null>(null))

  function onError(msg: string, err?: unknown) {
    const code = err instanceof ApiError ? err.status : 500
    globalError.value = { code, message: msg }
  }

  function clearError() {
    globalError.value = null
  }

  return { onError, clearError }
}
