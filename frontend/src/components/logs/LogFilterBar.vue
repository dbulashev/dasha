<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import type { LocationQuery } from 'vue-router'
import type { GetLogsServiceType } from '@/api/models'
import { copyToClipboard } from '@/utils/sql'
import { LOG_PRESETS, severityOptions, type LogFilters, type LogOrder } from './types'

const props = defineProps<{
  hosts: string[]
  loading: boolean
}>()

const emit = defineEmits<{
  search: [filters: LogFilters]
}>()

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const serviceType = ref<GetLogsServiceType>('postgresql')
const range = ref<string>('1h')
const severities = ref<string[]>([])
const host = ref<string>('')
const includes = ref<string[]>([])
const excludes = ref<string[]>([])
const database = ref<string>('')
const user = ref<string>('')
const dedup = ref<boolean>(false)
const pageSize = ref<number>(100)
const order = ref<LogOrder>('desc')
const preset = ref<string | null>(null)

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

const orderItems = computed(() => [
  { value: 'desc', title: t('logs.orderDesc') },
  { value: 'asc', title: t('logs.orderAsc') },
])

// Reset severities that are not valid for the newly selected service type.
watch(serviceType, () => {
  const allowed = new Set(severityItems.value)
  severities.value = (severities.value ?? []).filter(s => allowed.has(s))
})

const presetItems = computed(() =>
  LOG_PRESETS.map(p => ({ value: p.id, title: t(`logs.preset.${p.id}`) })),
)

function applyPreset(id: string | null) {
  const p = LOG_PRESETS.find(x => x.id === id)
  if (!p) return
  serviceType.value = 'postgresql'
  includes.value = p.message ? [p.message] : []
  severities.value = [...p.severities]
}

watch(preset, applyPreset)

// A selected preset stays selected only while the fields still match what it
// filled in — otherwise the select would advertise a filter set that is no
// longer active.
function sameArr(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i])
}

watch([serviceType, includes, severities], () => {
  const p = LOG_PRESETS.find(x => x.id === preset.value)
  if (!p) return
  const wantIncludes = p.message ? [p.message] : []
  const matches =
    serviceType.value === 'postgresql' &&
    sameArr(includes.value ?? [], wantIncludes) &&
    sameArr(severities.value ?? [], p.severities)
  if (!matches) preset.value = null
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

// Grafana time-picker clipboard interop ("Copy/Paste time range"):
// relative {"from":"now-1h","to":"now"} or absolute
// {"from":"2026-07-08 00:00:00","to":"2026-07-08 23:59:59"} in local time.
const rangeCopied = ref(false)

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

// Local wall-clock time in Grafana's absolute format.
function fmtGrafanaAbs(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// datetime-local input value for the custom range fields.
function fmtInputValue(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function copyGrafanaRange() {
  let payload: { from: string; to: string }
  if (range.value === 'custom') {
    if (rangeError.value) return
    payload = {
      from: fmtGrafanaAbs(new Date(customFrom.value)),
      to: fmtGrafanaAbs(new Date(customTo.value)),
    }
  } else {
    payload = { from: `now-${range.value}`, to: 'now' }
  }
  copyToClipboard(JSON.stringify(payload))
  rangeCopied.value = true
  setTimeout(() => {
    rangeCopied.value = false
  }, 1500)
}

// "2026-07-08 00:00:00" (and ISO variants) parsed as local time.
function parseGrafanaAbs(s: string): Date | null {
  const d = new Date(s.replace(' ', 'T'))
  return isNaN(d.getTime()) ? null : d
}

const REL_UNIT_MS: Record<string, number> = { m: 60_000, h: 3_600_000, d: 86_400_000, w: 7 * 86_400_000 }

async function pasteGrafanaRange() {
  let parsed: { from?: unknown; to?: unknown }
  try {
    parsed = JSON.parse(await navigator.clipboard.readText())
  } catch {
    return // clipboard unavailable or not Grafana's JSON — leave filters untouched
  }
  if (typeof parsed.from !== 'string' || typeof parsed.to !== 'string') return

  const rel = /^now-(\d+)([mhdw])$/.exec(parsed.from)
  if (parsed.to === 'now' && rel) {
    const key = `${rel[1]}${rel[2]}`
    if (rangeMs[key]) {
      range.value = key
      return
    }
    const ms = Number(rel[1]) * REL_UNIT_MS[rel[2]]
    const now = new Date()
    range.value = 'custom'
    customFrom.value = fmtInputValue(new Date(now.getTime() - ms))
    customTo.value = fmtInputValue(now)
    return
  }

  const from = parseGrafanaAbs(parsed.from)
  const to = parseGrafanaAbs(parsed.to)
  if (!from || !to) return
  range.value = 'custom'
  customFrom.value = fmtInputValue(from)
  customTo.value = fmtInputValue(to)
}

// Host/database/user are rarely used — they live behind a spoiler that opens
// automatically whenever any of them holds a value (restored state, drill-down).
const moreOpen = ref(false)
const hasMoreValues = computed(() => !!(host.value || database.value || user.value))

watch(hasMoreValues, v => {
  if (v) moreOpen.value = true
}, { immediate: true })

// Reset every filter field except the time range (and display prefs).
function clearFilters() {
  preset.value = null
  severities.value = []
  host.value = ''
  includes.value = []
  excludes.value = []
  database.value = ''
  user.value = ''
  dedup.value = false
}

// ---------------------------------------------------------------------------
// Form state (de)serialization — shared by the URL query string and the
// localStorage snapshot. "host" is renamed to log_host in the state because
// ?host= already belongs to the global host selector.

type FilterState = Record<string, string | string[]>

const STORAGE_KEY = 'dasha:logs:filters'

// Query-string keys owned by this form; everything else in route.query
// (cluster context like host/db) is preserved untouched on updates.
const LOG_QUERY_KEYS = [
  'service', 'range', 'from', 'to', 'severity', 'log_host',
  'message', 'exclude', 'database', 'user', 'dedup', 'page_size', 'order', 'preset',
]

function toState(): FilterState {
  const s: FilterState = {}
  if (serviceType.value !== 'postgresql') s.service = serviceType.value
  if (range.value === 'custom') {
    s.range = 'custom'
    s.from = customFrom.value
    s.to = customTo.value
  } else if (range.value !== '1h') {
    s.range = range.value
  }
  if (severities.value.length) s.severity = [...severities.value]
  if (host.value) s.log_host = host.value
  if (includes.value.length) s.message = [...includes.value]
  if (excludes.value.length) s.exclude = [...excludes.value]
  if (database.value) s.database = database.value
  if (user.value) s.user = user.value
  if (dedup.value) s.dedup = '1'
  if (pageSize.value !== 100) s.page_size = String(pageSize.value)
  if (order.value !== 'desc') s.order = order.value
  return s
}

function asArr(v: unknown): string[] {
  if (Array.isArray(v)) return v.filter((x): x is string => typeof x === 'string')
  return typeof v === 'string' && v !== '' ? [v] : []
}

function asStr(v: unknown): string {
  return typeof v === 'string' ? v : ''
}

function applyState(s: FilterState) {
  const svc = asStr(s.service)
  serviceType.value = svc === 'pooler' ? 'pooler' : 'postgresql'
  const r = asStr(s.range)
  if (r === 'custom' && asStr(s.from) && asStr(s.to)) {
    range.value = 'custom'
    customFrom.value = asStr(s.from)
    customTo.value = asStr(s.to)
  } else {
    range.value = rangeMs[r] ? r : '1h'
  }
  severities.value = asArr(s.severity)
  const h = asStr(s.log_host)
  host.value = props.hosts.includes(h) ? h : ''
  includes.value = asArr(s.message)
  excludes.value = asArr(s.exclude)
  database.value = asStr(s.database)
  user.value = asStr(s.user)
  dedup.value = asStr(s.dedup) === '1'
  const ps = Number(asStr(s.page_size))
  pageSize.value = pageSizeItems.includes(ps) ? ps : 100
  order.value = asStr(s.order) === 'asc' ? 'asc' : 'desc'
}

function syncQuery(state: FilterState) {
  const rest = Object.fromEntries(
    Object.entries(route.query).filter(([k]) => !LOG_QUERY_KEYS.includes(k)),
  )
  router.replace({ query: { ...rest, ...state } })
}

onMounted(() => {
  const q = route.query as LocationQuery
  const presetId = asStr(q.preset)
  if (presetId && LOG_PRESETS.some(p => p.id === presetId)) {
    preset.value = presetId
    // The preset watcher fires asynchronously; apply now so the deep link
    // searches with the preset already in place. Deep links investigate rare
    // events (deadlocks), so widen the default window from 1h to 24h.
    applyPreset(presetId)
    range.value = '24h'
    onSubmit()
    return
  }

  if (LOG_QUERY_KEYS.some(k => q[k] != null)) {
    applyState(q as FilterState)
    onSubmit()
    return
  }

  // No shared link — restore the last used filters, but don't auto-search:
  // every search costs an upstream Yandex API scan.
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved) {
      // Never restore concrete dates from a snapshot (incl. ones written
      // before date-stripping was added): a stale window would silently
      // search the wrong period.
      const { range: r, from: _from, to: _to, ...rest } = JSON.parse(saved) as FilterState
      applyState(r && r !== 'custom' ? { ...rest, range: r } : rest)
    }
  } catch {
    // Corrupted snapshot — start clean.
  }
})

// ---------------------------------------------------------------------------
// Stale indication: a dot on the search button while the form differs from the
// filters of the last executed search.

const appliedKey = ref('')

const currentKey = computed(() => JSON.stringify(toState()))

const dirty = computed(() => appliedKey.value !== '' && currentKey.value !== appliedKey.value)

function onSubmit() {
  const r = computeRange()
  if (!r) return

  const filters: LogFilters = {
    serviceType: serviceType.value,
    from: r.from,
    to: r.to,
    severities: [...(severities.value ?? [])],
    host: host.value ?? '',
    includes: (includes.value ?? []).map(s => s.trim()).filter(Boolean),
    excludes: (excludes.value ?? []).map(s => s.trim()).filter(Boolean),
    database: (database.value ?? '').trim(),
    user: (user.value ?? '').trim(),
    dedup: dedup.value,
    pageSize: pageSize.value,
    order: order.value,
  }

  const state = toState()
  appliedKey.value = currentKey.value
  syncQuery(state)
  try {
    // Concrete dates are session context, not a preference: restoring a stale
    // custom range days later would silently search the wrong window. Shared
    // links (the URL above) keep them; the snapshot drops them.
    const { from: _from, to: _to, ...persisted } = state
    if (persisted.range === 'custom') delete persisted.range
    localStorage.setItem(STORAGE_KEY, JSON.stringify(persisted))
  } catch {
    // Storage full/unavailable — persistence is best-effort.
  }

  emit('search', filters)
}

// ---------------------------------------------------------------------------
// Drill-down entry points used by the results table and the histogram.

function applyDrill(field: 'severity' | 'user' | 'database' | 'host', value: string) {
  if (!value) return
  switch (field) {
    case 'severity':
      severities.value = [value]
      break
    case 'user':
      user.value = value
      break
    case 'database':
      database.value = value
      break
    case 'host':
      if (!props.hosts.includes(value)) return
      host.value = value
      break
  }
  onSubmit()
}

function addExclude(text: string) {
  const v = text.trim()
  if (!v) return
  if (!excludes.value.includes(v)) excludes.value = [...excludes.value, v]
  onSubmit()
}

// Sets the custom period without searching: the dirty badge on the search
// button lights up and the user fires the (rate-limited) search deliberately.
function applyAbsoluteRange(fromIso: string, toIso: string) {
  const from = new Date(fromIso)
  const to = new Date(toIso)
  if (isNaN(from.getTime()) || isNaN(to.getTime()) || from >= to) return
  range.value = 'custom'
  customFrom.value = fmtInputValue(from)
  customTo.value = fmtInputValue(to)
}

defineExpose({ applyDrill, addExclude, applyAbsoluteRange })
</script>

<template>
  <v-card class="mb-4">
    <v-card-text>
      <v-row dense align="center">
        <v-col cols="12" sm="6" md="3" class="d-flex align-center justify-space-between">
          <v-icon icon="mdi-text-box-search-outline" size="28" color="primary" class="ms-1" />
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
          <div class="d-flex align-center ga-1">
            <v-select
              v-model="range"
              :items="rangeItems"
              :label="t('logs.timeRange')"
              density="compact"
              hide-details
            />
            <v-btn
              :icon="rangeCopied ? 'mdi-check' : 'mdi-content-copy'"
              size="small"
              variant="text"
              :disabled="rangeError"
              :title="t('logs.copyRange')"
              @click="copyGrafanaRange"
            />
            <v-btn
              icon="mdi-content-paste"
              size="small"
              variant="text"
              :title="t('logs.pasteRange')"
              @click="pasteGrafanaRange"
            />
          </div>
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
        <v-col cols="12" sm="6" md="2">
          <v-select
            v-model="preset"
            :items="presetItems"
            :label="t('logs.preset.label')"
            clearable
            density="compact"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="6" md="2">
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
        <v-col cols="12" sm="12" md="8">
          <div class="d-flex align-center ga-2">
            <v-combobox
              v-model="includes"
              :label="t('logs.message')"
              multiple
              chips
              closable-chips
              clearable
              density="compact"
              hide-details
              autocomplete="off"
              prepend-inner-icon="mdi-magnify"
              class="flex-grow-1"
            />
            <v-badge :model-value="!moreOpen && hasMoreValues" dot color="primary">
              <v-btn
                variant="text"
                size="small"
                :append-icon="moreOpen ? 'mdi-chevron-up' : 'mdi-chevron-down'"
                @click="moreOpen = !moreOpen"
              >
                {{ t('logs.moreFilters') }}
              </v-btn>
            </v-badge>
          </div>
        </v-col>
      </v-row>

      <v-expand-transition>
        <div v-show="moreOpen">
          <v-row dense align="center" class="mt-1">
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
            <v-col cols="12" sm="6" md="3">
              <v-text-field
                v-model="database"
                :label="t('logs.database')"
                clearable
                density="compact"
                hide-details
                autocomplete="off"
                @keyup.enter="onSubmit"
              />
            </v-col>
            <v-col cols="12" sm="6" md="3">
              <v-text-field
                v-model="user"
                :label="t('logs.user')"
                clearable
                density="compact"
                hide-details
                autocomplete="off"
                @keyup.enter="onSubmit"
              />
            </v-col>
          </v-row>
        </div>
      </v-expand-transition>

      <v-row dense align="center" class="mt-1">
        <v-col cols="12" sm="6" md="3">
          <v-combobox
            v-model="excludes"
            :label="t('logs.exclude')"
            multiple
            chips
            closable-chips
            clearable
            density="compact"
            hide-details
            autocomplete="off"
            prepend-inner-icon="mdi-magnify-remove-outline"
          />
        </v-col>
        <v-col cols="12" sm="4" md="2">
          <v-select
            v-model="pageSize"
            :items="pageSizeItems"
            :label="t('logs.pageSize')"
            density="compact"
            hide-details
          />
        </v-col>
        <v-col cols="12" sm="4" md="2">
          <v-select
            v-model="order"
            :items="orderItems"
            :label="t('logs.order')"
            :disabled="dedup"
            density="compact"
            hide-details
          />
        </v-col>
        <v-col cols="auto" class="ms-2">
          <v-switch
            v-model="dedup"
            :label="t('logs.dedup')"
            color="primary"
            density="compact"
            hide-details
          />
        </v-col>
        <v-spacer />
        <v-col cols="auto" class="text-right">
          <v-btn
            variant="text"
            icon="mdi-filter-remove-outline"
            size="small"
            class="me-2"
            :title="t('logs.clear')"
            @click="clearFilters"
          />
          <v-badge :model-value="dirty" dot color="error">
            <v-btn
              color="primary"
              :loading="props.loading"
              :disabled="rangeError"
              prepend-icon="mdi-magnify"
              @click="onSubmit"
            >
              {{ t('logs.search') }}
            </v-btn>
          </v-badge>
        </v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>
