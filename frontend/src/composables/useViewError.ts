import { ref } from 'vue'

export function useViewError() {
  const errorMessage = ref('')

  function onError(msg: string) {
    errorMessage.value = msg
  }

  function clearError() {
    errorMessage.value = ''
  }

  return { errorMessage, onError, clearError }
}
