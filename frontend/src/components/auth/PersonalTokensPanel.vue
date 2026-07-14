<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  listPersonalTokens,
  createPersonalToken,
  revokePersonalToken,
} from '@/api/gen/default/default'
import type {
  PersonalAccessToken,
  PersonalAccessTokenCreate,
  PersonalAccessTokenCreated,
  PersonalAccessTokenCreateRole,
} from '@/api/models'
import { useAuthStore } from '@/stores/auth'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { copyToClipboard } from '@/utils/sql'
import { fmtDateTime } from '@/utils/format'

const { t } = useI18n()
const authStore = useAuthStore()

const tokens = ref<PersonalAccessToken[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
const showRevoked = ref(false)

// Create form.
const name = ref('')
const role = ref<'viewer' | 'admin'>('viewer')
const expiresInDays = ref<number | null>(null)
const creating = ref(false)

// One-time secret reveal after a successful create.
const createdSecret = ref<string | null>(null)
const copied = ref(false)

const canPickAdmin = computed(() => authStore.user?.role === 'admin')

// The global VDataTable default is itemsPerPage: -1 with the footer hidden; page
// the list so a user with many tokens does not get an unbounded table. The
// revoked column only earns its width when revoked tokens are actually shown.
const headers = computed(() => [
  { title: t('pat.name'), key: 'name' },
  { title: t('pat.prefix'), key: 'prefix', sortable: false },
  { title: t('pat.role'), key: 'role' },
  { title: t('pat.created'), key: 'created_at' },
  { title: t('pat.lastUsed'), key: 'last_used_at' },
  { title: t('pat.expires'), key: 'expires_at' },
  ...(showRevoked.value ? [{ title: t('pat.revoked'), key: 'revoked_at' }] : []),
  { title: '', key: 'actions', sortable: false, align: 'end' as const },
])

async function load() {
  loading.value = true
  error.value = null
  try {
    tokens.value =
      assertOk<PersonalAccessToken[]>(
        await listPersonalTokens({ include_revoked: showRevoked.value }),
      ) ?? []
  } catch (e) {
    error.value = getErrorMessage(e)
  } finally {
    loading.value = false
  }
}

watch(showRevoked, load)

async function create() {
  if (!name.value.trim()) return
  if (expiresInDays.value != null && expiresInDays.value < 0) {
    error.value = t('pat.expiresNegative')
    return
  }
  creating.value = true
  error.value = null
  try {
    const body: PersonalAccessTokenCreate = {
      name: name.value.trim(),
      role: role.value as PersonalAccessTokenCreateRole,
    }
    if (expiresInDays.value && expiresInDays.value > 0) {
      body.expires_in_days = expiresInDays.value
    }
    const created = assertOk<PersonalAccessTokenCreated>(await createPersonalToken(body))
    createdSecret.value = created.token
    name.value = ''
    expiresInDays.value = null
    role.value = 'viewer'
    await load()
  } catch (e) {
    error.value = getErrorMessage(e)
  } finally {
    creating.value = false
  }
}

async function revoke(id: string) {
  error.value = null
  try {
    assertOk(await revokePersonalToken(id))
    await load()
  } catch (e) {
    error.value = getErrorMessage(e)
  }
}

function copySecret() {
  if (!createdSecret.value) return
  // copyToClipboard has an execCommand fallback for insecure (http://) contexts
  // where navigator.clipboard is undefined — losing a one-time secret otherwise.
  copyToClipboard(createdSecret.value)
  copied.value = true
  setTimeout(() => (copied.value = false), 1500)
}

onMounted(load)
</script>

<template>
  <v-alert v-if="error" type="error" variant="tonal" density="compact" class="mb-3">
    {{ error }}
  </v-alert>

  <!-- One-time secret reveal -->
  <v-alert
    v-if="createdSecret"
    type="warning"
    variant="tonal"
    border="start"
    icon="mdi-content-copy"
    class="mb-4"
  >
    <div class="font-weight-medium mb-1">{{ t('pat.secretOnceTitle') }}</div>
    <div class="text-body-2 mb-2">{{ t('pat.secretOnce') }}</div>
    <div class="d-flex align-center ga-2">
      <code class="pat-secret flex-grow-1">{{ createdSecret }}</code>
      <v-btn
        size="small"
        variant="tonal"
        :prepend-icon="copied ? 'mdi-check' : 'mdi-content-copy'"
        @click="copySecret"
      >
        {{ copied ? t('pat.copied') : t('pat.copy') }}
      </v-btn>
    </div>
  </v-alert>

  <!-- Create form -->
  <div class="text-subtitle-2 mb-2">{{ t('pat.newToken') }}</div>
  <v-row dense>
    <v-col cols="12" sm="6">
      <v-text-field
        v-model="name"
        :label="t('pat.name')"
        density="compact"
        hide-details
        maxlength="64"
      />
    </v-col>
    <v-col v-if="canPickAdmin" cols="6" sm="3">
      <v-select
        v-model="role"
        :items="['viewer', 'admin']"
        :label="t('pat.role')"
        density="compact"
        hide-details
      />
    </v-col>
    <v-col cols="6" :sm="canPickAdmin ? 3 : 6">
      <v-text-field
        v-model.number="expiresInDays"
        type="number"
        min="0"
        :label="t('pat.expiresInDays')"
        :placeholder="t('pat.never')"
        density="compact"
        hide-details
      />
    </v-col>
  </v-row>
  <div class="d-flex justify-end mt-2 mb-1">
    <v-btn
      color="primary"
      variant="flat"
      :loading="creating"
      :disabled="!name.trim()"
      @click="create"
    >
      {{ t('pat.create') }}
    </v-btn>
  </div>

  <v-divider class="my-3" />

  <!-- Existing tokens -->
  <div class="d-flex justify-end">
    <v-checkbox
      v-model="showRevoked"
      :label="t('pat.showRevoked')"
      density="compact"
      hide-details
    />
  </div>

  <v-data-table
    :headers="headers"
    :items="tokens"
    :loading="loading"
    :items-per-page="10"
    :hide-default-footer="false"
    item-value="id"
    hover
  >
    <template #no-data>
      <div class="py-4 text-medium-emphasis">{{ t('pat.empty') }}</div>
    </template>
    <template #item.name="{ item }">
      <span :class="{ 'text-disabled': item.revoked_at }">{{ item.name }}</span>
    </template>
    <template #item.prefix="{ item }">
      <code class="text-caption">{{ item.prefix }}…</code>
    </template>
    <template #item.role="{ item }">
      <v-chip size="x-small" variant="tonal">{{ item.role }}</v-chip>
    </template>
    <template #item.revoked_at="{ item }">
      <v-chip v-if="item.revoked_at" size="x-small" variant="tonal" color="error">
        {{ fmtDateTime(item.revoked_at) }}
      </v-chip>
      <span v-else class="text-caption text-medium-emphasis">—</span>
    </template>
    <template #item.created_at="{ item }">
      <span class="text-caption">{{ fmtDateTime(item.created_at) }}</span>
    </template>
    <template #item.last_used_at="{ item }">
      <span class="text-caption">{{ fmtDateTime(item.last_used_at) }}</span>
    </template>
    <template #item.expires_at="{ item }">
      <span class="text-caption">{{ fmtDateTime(item.expires_at) }}</span>
    </template>
    <template #item.actions="{ item }">
      <v-btn
        v-if="!item.revoked_at"
        size="x-small"
        variant="text"
        color="error"
        prepend-icon="mdi-delete-outline"
        @click="revoke(item.id)"
      >
        {{ t('pat.revoke') }}
      </v-btn>
    </template>
  </v-data-table>
</template>

<style scoped>
.pat-secret {
  font-family: 'Roboto Mono', ui-monospace, monospace;
  font-size: 0.85rem;
  background: rgba(var(--v-theme-on-surface), 0.06);
  padding: 6px 10px;
  border-radius: 4px;
  word-break: break-all;
}
</style>
