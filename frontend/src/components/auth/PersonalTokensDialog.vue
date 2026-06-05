<script setup lang="ts">
import { ref, computed, watch } from 'vue'
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

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [boolean] }>()

const { t } = useI18n()
const authStore = useAuthStore()

const open = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const tokens = ref<PersonalAccessToken[]>([])
const loading = ref(false)
const error = ref<string | null>(null)

// Create form.
const name = ref('')
const role = ref<'viewer' | 'admin'>('viewer')
const expiresInDays = ref<number | null>(null)
const creating = ref(false)

// One-time secret reveal after a successful create.
const createdSecret = ref<string | null>(null)
const copied = ref(false)

const canPickAdmin = computed(() => authStore.user?.role === 'admin')

async function load() {
  loading.value = true
  error.value = null
  try {
    tokens.value = assertOk<PersonalAccessToken[]>(await listPersonalTokens()) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

async function create() {
  if (!name.value.trim()) return
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
    error.value = e instanceof Error ? e.message : String(e)
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
    error.value = e instanceof Error ? e.message : String(e)
  }
}

async function copySecret() {
  if (!createdSecret.value) return
  try {
    await navigator.clipboard.writeText(createdSecret.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch {
    // Clipboard unavailable; ignore.
  }
}

function fmtDate(s?: string | null): string {
  return s ? new Date(s).toLocaleString() : '—'
}

// Reset transient state and (re)load whenever the dialog opens.
watch(open, (v) => {
  if (v) {
    createdSecret.value = null
    copied.value = false
    load()
  }
})
</script>

<template>
  <v-dialog v-model="open" max-width="760" scrollable>
    <v-card>
      <v-card-title class="d-flex align-center ga-2">
        <v-icon icon="mdi-key-chain-variant" />
        {{ t('pat.title') }}
        <v-spacer />
        <v-btn icon="mdi-close" variant="text" size="small" @click="open = false" />
      </v-card-title>
      <v-divider />

      <v-card-text>
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
        <v-row dense class="mb-2">
          <v-col cols="12" sm="5">
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
          <v-col cols="6" sm="3">
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
          <v-col cols="12" sm="1" class="d-flex align-center">
            <v-btn
              color="primary"
              variant="flat"
              :loading="creating"
              :disabled="!name.trim()"
              block
              @click="create"
            >
              {{ t('pat.create') }}
            </v-btn>
          </v-col>
        </v-row>

        <v-divider class="my-3" />

        <!-- Existing tokens -->
        <v-progress-linear v-if="loading" indeterminate height="2" />
        <v-alert
          v-else-if="!tokens.length"
          type="info"
          variant="tonal"
          density="compact"
        >
          {{ t('pat.empty') }}
        </v-alert>
        <v-table v-else density="compact">
          <thead>
            <tr>
              <th>{{ t('pat.name') }}</th>
              <th>{{ t('pat.prefix') }}</th>
              <th>{{ t('pat.role') }}</th>
              <th>{{ t('pat.lastUsed') }}</th>
              <th>{{ t('pat.expires') }}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            <tr v-for="tok in tokens" :key="tok.id">
              <td>{{ tok.name }}</td>
              <td><code class="text-caption">{{ tok.prefix }}…</code></td>
              <td>
                <v-chip size="x-small" variant="tonal">{{ tok.role }}</v-chip>
              </td>
              <td class="text-caption">{{ fmtDate(tok.last_used_at) }}</td>
              <td class="text-caption">{{ fmtDate(tok.expires_at) }}</td>
              <td class="text-right">
                <v-btn
                  size="x-small"
                  variant="text"
                  color="error"
                  prepend-icon="mdi-delete-outline"
                  @click="revoke(tok.id)"
                >
                  {{ t('pat.revoke') }}
                </v-btn>
              </td>
            </tr>
          </tbody>
        </v-table>
      </v-card-text>
    </v-card>
  </v-dialog>
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
