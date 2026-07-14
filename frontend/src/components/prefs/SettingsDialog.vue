<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { usePrefsStore } from '@/stores/prefs'
import { useLocaleStore, LOCALES, type LocaleSetting } from '@/stores/locale'
import { PAGE_SIZE_OPTIONS } from '@/constants/pagination'
import { TIME_ZONES, tzTitle } from '@/constants/timezones'
import PersonalTokensPanel from '@/components/auth/PersonalTokensPanel.vue'
import AdminTokensPanel from '@/components/auth/AdminTokensPanel.vue'
import UsersPanel from '@/components/auth/UsersPanel.vue'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [boolean] }>()

const { t } = useI18n()
const authStore = useAuthStore()
const themeStore = useThemeStore()
const localeStore = useLocaleStore()
const prefs = usePrefsStore()

// Local first (the default), then UTC and the Russian zones by IANA id.
const timezones = computed(() => [
  { value: 'local', title: t('prefs.timezoneLocal') },
  ...TIME_ZONES.map((tz) => ({ value: tz, title: tzTitle(tz) })),
])

// 'system' follows the OS preference — the old toolbar toggle only flipped
// light/dark, so it was a one-way door out of it.
const themes = computed(() => [
  { value: 'system', title: t('prefs.themeSystem'), icon: 'mdi-monitor' },
  { value: 'light', title: t('prefs.themeLight'), icon: 'mdi-weather-sunny' },
  { value: 'dark', title: t('prefs.themeDark'), icon: 'mdi-weather-night' },
])

const open = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const tab = ref('interface')

const language = computed<LocaleSetting>({
  get: () => localeStore.currentLocale(),
  set: (v) => localeStore.setLocale(v),
})

// The panels fetch on mount, so remounting them on each open keeps the lists
// fresh without a manual refresh.
watch(open, (v) => {
  if (v) tab.value = 'interface'
})
</script>

<template>
  <v-dialog v-model="open" max-width="900" scrollable>
    <v-card>
      <v-card-title class="d-flex align-center ga-2">
        <v-icon icon="mdi-cog-outline" />
        {{ t('prefs.title') }}
        <v-spacer />
        <v-btn icon="mdi-close" variant="text" size="small" @click="open = false" />
      </v-card-title>
      <v-divider />

      <v-tabs v-model="tab" density="compact">
        <v-tab value="interface" prepend-icon="mdi-monitor">{{ t('prefs.tabInterface') }}</v-tab>
        <v-tab v-if="authStore.canManageTokens" value="tokens" prepend-icon="mdi-key-chain-variant">
          {{ t('prefs.tabTokens') }}
        </v-tab>
        <v-tab v-if="authStore.canAdminTokens" value="adminTokens" prepend-icon="mdi-shield-key">
          {{ t('prefs.tabAdminTokens') }}
        </v-tab>
        <v-tab v-if="authStore.canAdminTokens" value="users" prepend-icon="mdi-account-group">
          {{ t('prefs.tabUsers') }}
        </v-tab>
      </v-tabs>
      <v-divider />

      <v-card-text style="min-height: 340px">
        <v-tabs-window v-model="tab" class="pt-4">
          <v-tabs-window-item value="interface">
            <v-select
              v-model="language"
              :items="LOCALES"
              item-title="label"
              item-value="value"
              :label="t('prefs.language')"
              :hint="t('prefs.languageHint')"
              persistent-hint
              density="compact"
              prepend-inner-icon="mdi-translate"
              style="max-width: 320px"
            />

            <v-select
              v-model="themeStore.theme"
              :items="themes"
              :label="t('prefs.theme')"
              density="compact"
              prepend-inner-icon="mdi-theme-light-dark"
              class="mt-6"
              style="max-width: 320px"
            >
              <template #item="{ props: itemProps, item }">
                <v-list-item v-bind="itemProps" :prepend-icon="item.raw.icon" />
              </template>
            </v-select>

            <v-select
              v-model="prefs.timezone"
              :items="timezones"
              :label="t('prefs.timezone')"
              density="compact"
              prepend-inner-icon="mdi-clock-outline"
              class="mt-6"
              style="max-width: 320px"
            />

            <v-select
              v-model="prefs.pageSize"
              :items="PAGE_SIZE_OPTIONS"
              :label="t('prefs.pageSize')"
              :hint="t('prefs.pageSizeHint')"
              persistent-hint
              density="compact"
              prepend-inner-icon="mdi-table-row"
              class="mt-6"
              style="max-width: 320px"
            />
          </v-tabs-window-item>

          <v-tabs-window-item v-if="authStore.canManageTokens" value="tokens">
            <PersonalTokensPanel v-if="tab === 'tokens'" />
          </v-tabs-window-item>

          <v-tabs-window-item v-if="authStore.canAdminTokens" value="adminTokens">
            <AdminTokensPanel v-if="tab === 'adminTokens'" />
          </v-tabs-window-item>

          <v-tabs-window-item v-if="authStore.canAdminTokens" value="users">
            <UsersPanel v-if="tab === 'users'" />
          </v-tabs-window-item>
        </v-tabs-window>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>
