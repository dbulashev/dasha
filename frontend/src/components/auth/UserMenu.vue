<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { AuthInfoMode } from '@/api/models'
import PersonalTokensDialog from './PersonalTokensDialog.vue'

const { t } = useI18n()
const authStore = useAuthStore()

const tokensOpen = ref(false)
const menuOpen = ref(false)

function openTokens() {
  menuOpen.value = false
  tokensOpen.value = true
}
</script>

<template>
  <template v-if="authStore.mode === AuthInfoMode.oidc">
    <template v-if="authStore.user">
      <v-menu v-model="menuOpen" location="bottom end" :close-on-content-click="false">
        <template #activator="{ props }">
          <v-btn v-bind="props" icon variant="text" class="ml-1">
            <v-icon>mdi-account-circle</v-icon>
          </v-btn>
        </template>
        <v-card min-width="220">
          <v-card-text class="text-center py-4">
            <v-avatar color="primary" size="48" class="mb-2">
              <span class="text-h6">{{ authStore.user.name?.charAt(0)?.toUpperCase() }}</span>
            </v-avatar>
            <div class="text-subtitle-1 font-weight-medium">{{ authStore.user.name }}</div>
            <div class="text-caption text-medium-emphasis">{{ authStore.user.email }}</div>
            <v-chip size="x-small" variant="tonal" class="mt-1">{{ authStore.user.role }}</v-chip>
          </v-card-text>
          <v-divider />
          <v-card-actions class="d-flex flex-column ga-1 pa-2">
            <v-btn
              v-if="authStore.patEnabled"
              block
              variant="text"
              prepend-icon="mdi-key-chain-variant"
              @click="openTokens"
            >
              {{ t('pat.menuItem') }}
            </v-btn>
            <v-btn block variant="text" prepend-icon="mdi-logout" @click="authStore.logout">
              {{ t('Logout') }}
            </v-btn>
          </v-card-actions>
        </v-card>
      </v-menu>

      <PersonalTokensDialog v-model="tokensOpen" />
    </template>
    <v-btn v-else icon variant="text" class="ml-1" @click="authStore.doLoginRedirect">
      <v-icon>mdi-login</v-icon>
    </v-btn>
  </template>
</template>
