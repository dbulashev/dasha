import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useThemeStore = defineStore('theme_key', () => {
  // 'system' follows the OS preference; the settings dialog writes this directly.
  const theme = ref<'light' | 'dark' | 'system'>('system')

  // Resolves 'system' for consumers that need a concrete light/dark value
  // (chart palettes, which cannot read a CSS variable).
  function currentTheme(): 'light' | 'dark' {
    if (theme.value === 'system') {
      const prefersDark = typeof window !== 'undefined' && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
      return prefersDark ? 'dark' : 'light'
    }
    return theme.value
  }

  return { theme, currentTheme }
}, {
  persist: {
    storage: localStorage,
  },
})

