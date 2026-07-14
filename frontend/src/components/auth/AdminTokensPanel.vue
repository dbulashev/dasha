<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listAllPersonalTokens, revokeAnyPersonalToken } from '@/api/gen/default/default'
import type { AdminPersonalAccessToken } from '@/api/models'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtDateTime } from '@/utils/format'

const { t } = useI18n()

const tokens = ref<AdminPersonalAccessToken[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
const search = ref('')
const showRevoked = ref(false)

// Revoking someone else's token is irreversible and cannot be undone from the UI,
// so it goes through an explicit confirmation naming the token and its owner.
const pending = ref<AdminPersonalAccessToken | null>(null)
const revoking = ref(false)

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return tokens.value
  return tokens.value.filter(
    (tok) => tok.name.toLowerCase().includes(q) || tok.owner.toLowerCase().includes(q),
  )
})

// The global VDataTable default is itemsPerPage: -1 with the footer hidden; this
// list spans every owner and grows unbounded, so it opts back into paging. The
// revoked column only earns its width when revoked tokens are actually shown.
const headers = computed(() => [
  { title: t('admin.tokens.owner'), key: 'owner' },
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
      assertOk<AdminPersonalAccessToken[]>(
        await listAllPersonalTokens({ include_revoked: showRevoked.value }),
      ) ?? []
  } catch (e) {
    error.value = getErrorMessage(e)
  } finally {
    loading.value = false
  }
}

watch(showRevoked, load)

async function confirmRevoke() {
  if (!pending.value) return
  revoking.value = true
  error.value = null
  try {
    assertOk(await revokeAnyPersonalToken(pending.value.id))
    pending.value = null
    await load()
  } catch (e) {
    error.value = getErrorMessage(e)
  } finally {
    revoking.value = false
  }
}

onMounted(load)
</script>

<template>
  <v-alert v-if="error" type="error" variant="tonal" density="compact" class="mb-3">
    {{ error }}
  </v-alert>

  <div class="d-flex align-center ga-4 mb-3">
    <div class="text-subtitle-2">{{ t('admin.tokens.title') }}</div>
    <v-spacer />
    <v-checkbox
      v-model="showRevoked"
      :label="t('pat.showRevoked')"
      density="compact"
      hide-details
    />
    <v-text-field
      v-model="search"
      :label="t('admin.tokens.search')"
      prepend-inner-icon="mdi-magnify"
      density="compact"
      hide-details
      clearable
      style="max-width: 280px"
    />
  </div>

  <v-data-table
    :headers="headers"
    :items="filtered"
    :loading="loading"
    :items-per-page="10"
    :hide-default-footer="false"
    item-value="id"
    hover
  >
    <template #no-data>
      <div class="py-4 text-medium-emphasis">{{ t('admin.tokens.empty') }}</div>
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
        @click="pending = item"
      >
        {{ t('admin.tokens.revoke') }}
      </v-btn>
    </template>
  </v-data-table>

  <v-dialog :model-value="!!pending" max-width="480" @update:model-value="pending = null">
    <v-card>
      <v-card-text>
        {{
          pending ? t('admin.tokens.revokeConfirm', { name: pending.name, owner: pending.owner }) : ''
        }}
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="pending = null">{{ t('Cancel') }}</v-btn>
        <v-btn color="error" variant="flat" :loading="revoking" @click="confirmRevoke">
          {{ t('admin.tokens.revoke') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
