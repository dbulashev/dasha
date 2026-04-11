<script setup lang="ts">
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const authStore = useAuthStore()
const appVersion = import.meta.env.VITE_APP_VERSION || 'dev'
</script>

<template>
  <v-container class="fill-height d-flex flex-column align-center justify-center">
    <v-card max-width="440" class="pa-8 text-center" elevation="4" theme="light">
      <img src="/logo.png" alt="Dasha" class="login-logo mb-8" />
      <v-btn
        color="primary"
        size="large"
        block
        prepend-icon="mdi-login"
        @click="authStore.doLoginRedirect"
      >
        {{ t('Sign in with SSO') }}
      </v-btn>
    </v-card>
    <div class="text-caption text-medium-emphasis mt-1">{{ appVersion }}</div>
  </v-container>
</template>

<style scoped>
.login-logo {
  max-width: 340px;
  height: auto;
}
</style>
