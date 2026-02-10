import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useThemeStore = defineStore('theme_key', () => {
  const theme = ref<'light' | 'dark' | 'system'>('system')
  function toggleTheme() {
    theme.value = currentTheme() === 'dark' ? 'light' : 'dark'
  }

  function currentTheme(): 'light' | 'dark' {
    if (theme.value === 'system') {
      const prefersDark = typeof window !== 'undefined' && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
      return prefersDark ? 'dark' : 'light'
    }
    return theme.value
  }

  function icon(): 'mdi-weather-sunny' | 'mdi-weather-night' {
    return currentTheme() === 'dark' ? 'mdi-weather-sunny' : 'mdi-weather-night'
  }

  return { theme, toggleTheme, currentTheme, icon }
}, {
  persist: {
    storage: localStorage,
  },
})

