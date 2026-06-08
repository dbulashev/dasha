<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GetLogsServiceType } from '@/api/models'
import { severityOptions, type LogFilters } from './types'

const props = defineProps<{
  hosts: string[]
  loading: boolean
}>()

const emit = defineEmits<{
  search: [filters: LogFilters]
}>()

const { t } = useI18n()

const serviceType = ref<GetLogsServiceType>('postgresql')
const range = ref<string>('1h')
const severities = ref<string[]>([])
const host = ref<string>('')
const message = ref<string>('')
const database = ref<string>('')
const user = ref<string>('')
const dedup = ref<boolean>(false)
const pageSize = ref<number>(100)

// Custom range bounds (datetime-local strings).
const customFrom = ref<string>('')
const customTo = ref<string>('')

const serviceTypeItems = [
  { value: 'postgresql', title: 'PostgreSQL' },
  { value: 'pooler', title: 'Pooler' },
]

const rangeItems = computed(() => [
  { value: '1h', title: t('logs.range.1h') },
  { value: '6h', title: t('logs.range.6h') },
  { value: '24h', title: t('logs.range.24h') },
  { value: '7d', title: t('logs.range.7d') },
  { value: 'custom', title: t('logs.range.custom') },
])

const severityItems = computed(() => severityOptions(serviceType.value))

const pageSizeItems = [50, 100, 250, 500, 1000]

// Reset severities that are not valid for the newly selected service type.
watch(serviceType, () => {
  const allowed = new Set(severityItems.value)
  severities.value = (severities.value ?? []).filter(s => allowed.has(s))
})

const rangeMs: Record<string, number> = {
  '1h': 60 * 60 * 1000,
  '6h': 6 * 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
}

function computeRange(): { from: string; to: string } | null {
  if (range.value === 'custom') {
    if (!customFrom.value || !customTo.value) return null
    const from = new Date(customFrom.value)
    const to = new Date(customTo.value)
    if (isNaN(from.getTime()) || isNaN(to.getTime()) || from >= to) return null
    return { from: from.toISOString(), to: to.toISOString() }
  }
  const ms = rangeMs[range.value] ?? rangeMs['1h']
  const now = Date.now()
  return { from: new Date(now - ms).toISOString(), to: new Date(now).toISOString() }
}

const rangeError = computed(() => range.value === 'custom' && computeRange() === null)

function onSubmit() {
  const r = computeRange()
  if (!r) return

  const filters: LogFilters = {
    serviceType: serviceType.value,
    from: r.from,
    to: r.to,
    severities: [...(severities.value ?? [])],
    host: host.value ?? '',
    message: (message.value ?? '').trim(),
    database: (database.value ?? '').trim(),
    user: (user.value ?? '').trim(),
    dedup: dedup.value,
    pageSize: pageSize.value,
  }

  emit('search', filters)
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-text>
      <v-row dense align="center">
        <v-col cols="12" sm="6" md="3">
          <v-btn-toggle
            v-model="serviceType"
            mandatory
            density="comfortable"
            color="primary"
            variant="outlined"
          >
            <v-btn
              v-for="st in serviceTypeItems"
              :key="st.value"
              :value="st.value"
              size="small"
            >
              {{ st.title }}
            </v-btn>
          </v-btn-toggle>
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-select
            v-model="range"
            :items="rangeItems"
            :label="t('logs.timeRange')"
            density="compact"
            hide-details
          />
        </v-col>
        <template v-if="range === 'custom'">
          <v-col cols="12" sm="6" md="3">
            <v-text-field
              v-model="customFrom"
              type="datetime-local"
              :label="t('logs.from')"
              density="compact"
              hide-details
              :error="rangeError"
            />
          </v-col>
          <v-col cols="12" sm="6" md="3">
            <v-text-field
              v-model="customTo"
              type="datetime-local"
              :label="t('logs.to')"
              density="compact"
              hide-details
              :error="rangeError"
            />
          </v-col>
        </template>
      </v-row>

      <v-row dense align="center" class="mt-1">
        <v-col cols="12" sm="6" md="3">
          <v-select
            v-model="severities"
            :items="severityItems"
            :label="t('logs.severity')"
            multiple
            chips
            closable-chips
            clearable
            density="compact"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="6" md="3">
          <v-select
            v-model="host"
            :items="props.hosts"
            :label="t('logs.host')"
            clearable
            density="compact"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="6" md="2">
          <v-text-field
            v-model="database"
            :label="t('logs.database')"
            clearable
            density="compact"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="6" md="2">
          <v-text-field
            v-model="user"
            :label="t('logs.user')"
            clearable
            density="compact"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="6" md="2">
          <v-text-field
            v-model="message"
            :label="t('logs.message')"
            clearable
            density="compact"
            hide-details
            prepend-inner-icon="mdi-magnify"
          />
        </v-col>
      </v-row>

      <v-row dense align="center" class="mt-1">
        <v-col cols="12" sm="4" md="2">
          <v-select
            v-model="pageSize"
            :items="pageSizeItems"
            :label="t('logs.pageSize')"
            density="compact"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="4" md="3">
          <v-switch
            v-model="dedup"
            :label="t('logs.dedup')"
            color="primary"
            density="compact"
            hide-details
          />
        </v-col>
        <v-spacer />
        <v-col cols="12" sm="4" md="3" class="text-right">
          <v-btn
            color="primary"
            :loading="props.loading"
            :disabled="rangeError"
            prepend-icon="mdi-magnify"
            @click="onSubmit"
          >
            {{ t('logs.search') }}
          </v-btn>
        </v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>
