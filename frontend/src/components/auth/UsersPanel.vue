<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { listUsers } from '@/api/gen/default/default'
import type { AdminUser } from '@/api/models'
import { assertOk } from '@/utils/api'
import { getErrorMessage } from '@/utils/error'
import { fmtDateTime } from '@/utils/format'

const { t } = useI18n()

const users = ref<AdminUser[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
const search = ref('')

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return users.value
  return users.value.filter(
    (u) => u.subject.toLowerCase().includes(q) || u.name.toLowerCase().includes(q),
  )
})

// The global VDataTable default is itemsPerPage: -1 with the footer hidden; the
// user list grows with every person who signs in, so it opts back into paging.
const headers = computed(() => [
  { title: t('admin.users.subject'), key: 'subject' },
  { title: t('admin.users.name'), key: 'name' },
  { title: t('admin.users.role'), key: 'role' },
  { title: t('admin.users.tokens'), key: 'tokens', align: 'end' as const },
  { title: t('admin.users.createdAt'), key: 'created_at' },
  { title: t('admin.users.lastLogin'), key: 'last_login_at' },
])

async function load() {
  loading.value = true
  error.value = null
  try {
    users.value = assertOk<AdminUser[]>(await listUsers()) ?? []
  } catch (e) {
    error.value = getErrorMessage(e)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <v-alert v-if="error" type="error" variant="tonal" density="compact" class="mb-3">
    {{ error }}
  </v-alert>

  <div class="d-flex align-center ga-2 mb-1">
    <div class="text-subtitle-2">{{ t('admin.users.title') }}</div>
    <v-spacer />
    <v-text-field
      v-model="search"
      :label="t('admin.users.search')"
      prepend-inner-icon="mdi-magnify"
      density="compact"
      hide-details
      clearable
      style="max-width: 280px"
    />
  </div>
  <div class="text-caption text-medium-emphasis mb-3">{{ t('admin.users.hint') }}</div>

  <v-data-table
    :headers="headers"
    :items="filtered"
    :loading="loading"
    :items-per-page="10"
    :hide-default-footer="false"
    item-value="subject"
    hover
  >
    <template #no-data>
      <div class="py-4 text-medium-emphasis">{{ t('admin.users.empty') }}</div>
    </template>
    <template #item.role="{ item }">
      <v-chip size="x-small" variant="tonal">{{ item.role }}</v-chip>
    </template>
    <template #item.created_at="{ item }">
      <span class="text-caption">{{ fmtDateTime(item.created_at) }}</span>
    </template>
    <template #item.last_login_at="{ item }">
      <span class="text-caption">
        {{ item.last_login_at ? fmtDateTime(item.last_login_at) : t('admin.users.never') }}
      </span>
    </template>
  </v-data-table>
</template>
