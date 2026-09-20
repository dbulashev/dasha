<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LogCategory } from '@/api/models'
import { useThemeStore } from '@/stores/theme'
import { fmtCompact, fmtDateTime, fmtPct } from '@/utils/format'

const props = defineProps<{
  categories: LogCategory[]
  from?: string
  to?: string
}>()

const { t, te } = useI18n()
const themeStore = useThemeStore()

const COLORS_LIGHT: Record<string, string> = {
  deadlock: '#E53935',
  lock_wait: '#FB8C00',
  connection_limit: '#F4511E',
  authentication: '#8E24AA',
  canceled: '#FDD835',
  plan: '#1E88E5',
  slow_query: '#3949AB',
  checkpoint: '#00897B',
  autovacuum: '#43A047',
  temp_file: '#6D4C41',
  connection: '#546E7A',
  error: '#C62828',
  other: '#9E9E9E',
}

const COLORS_DARK: Record<string, string> = {
  deadlock: '#EF5350',
  lock_wait: '#FFA726',
  connection_limit: '#FF7043',
  authentication: '#AB47BC',
  canceled: '#FFEE58',
  plan: '#42A5F5',
  slow_query: '#5C6BC0',
  checkpoint: '#26A69A',
  autovacuum: '#66BB6A',
  temp_file: '#8D6E63',
  connection: '#78909C',
  error: '#E53935',
  other: '#BDBDBD',
}

const isDark = computed(() => themeStore.currentTheme() === 'dark')
const colors = computed(() => (isDark.value ? COLORS_DARK : COLORS_LIGHT))

function color(code: string): string {
  return colors.value[code] ?? colors.value.other
}

function label(code: string): string {
  const key = `logCategory.${code}`
  return te(key) ? t(key) : code
}

const windowMs = computed(() => {
  const from = Date.parse(props.from ?? '')
  const to = Date.parse(props.to ?? '')
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) return 0
  return to - from
})

const BURST_SHARE = 0.1
const BURST_MIN_COUNT = 10

// A category whose records all landed in a small part of the window is an
// episode, not a background rate; the share column alone cannot tell them apart.
function burstSpanMs(c: LogCategory): number | null {
  if (windowMs.value === 0 || c.count < BURST_MIN_COUNT) return null
  const first = Date.parse(c.first_seen)
  const last = Date.parse(c.last_seen)
  if (!Number.isFinite(first) || !Number.isFinite(last)) return null
  const span = last - first
  return span <= windowMs.value * BURST_SHARE ? span : null
}

function burstText(c: LogCategory): string {
  const span = burstSpanMs(c)
  if (span == null) return ''
  return t('logs.insights.burst', { minutes: Math.max(1, Math.round(span / 60000)) })
}

const expanded = ref<string[]>([])

function toggle(code: string) {
  expanded.value = expanded.value.includes(code)
    ? expanded.value.filter(c => c !== code)
    : [...expanded.value, code]
}
</script>

<template>
  <v-card class="mb-4">
    <v-card-text>
      <div v-if="categories.length" class="d-flex mb-4 category-bar">
        <div
          v-for="c in categories"
          :key="c.code"
          :style="{ width: `${Math.max(c.share * 100, 0.5)}%`, background: color(c.code) }"
          class="category-bar-part"
        >
          <v-tooltip activator="parent" location="top">
            {{ label(c.code) }}: {{ fmtCompact(c.count) }} ({{ fmtPct(c.share * 100) }})
          </v-tooltip>
        </div>
      </div>

      <v-table density="compact">
        <thead>
          <tr>
            <th>{{ t('logs.insights.categories.category') }}</th>
            <th class="text-right">{{ t('logs.col.count') }}</th>
            <th class="text-right">{{ t('logs.insights.categories.share') }}</th>
            <th>{{ t('logs.insights.firstSeen') }}</th>
            <th>{{ t('logs.col.lastSeen') }}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <template v-for="c in categories" :key="c.code">
            <tr :class="{ 'category-row': c.templates.length }" @click="c.templates.length && toggle(c.code)">
              <td>
                <v-icon :color="color(c.code)" icon="mdi-square" size="x-small" class="me-2" />
                {{ label(c.code) }}
                <v-chip v-if="burstText(c)" color="warning" size="x-small" variant="tonal" label class="ms-2">
                  {{ burstText(c) }}
                </v-chip>
              </td>
              <td class="text-right">{{ fmtCompact(c.count) }}</td>
              <td class="text-right">{{ fmtPct(c.share * 100) }}</td>
              <td>{{ fmtDateTime(c.first_seen) }}</td>
              <td>{{ fmtDateTime(c.last_seen) }}</td>
              <td class="text-right">
                <v-icon
                  v-if="c.templates.length"
                  :icon="expanded.includes(c.code) ? 'mdi-chevron-up' : 'mdi-chevron-down'"
                  size="small"
                />
              </td>
            </tr>
            <tr v-if="expanded.includes(c.code)">
              <td colspan="6" class="pa-0">
                <v-table density="compact" class="category-templates">
                  <tbody>
                    <tr v-for="(tpl, i) in c.templates" :key="i">
                      <td class="text-mono text-caption">{{ tpl.template }}</td>
                      <td class="text-right text-caption">{{ fmtCompact(tpl.count) }}</td>
                      <td class="text-caption">{{ fmtDateTime(tpl.last_seen) }}</td>
                    </tr>
                  </tbody>
                </v-table>
              </td>
            </tr>
          </template>
        </tbody>
      </v-table>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.category-bar {
  height: 14px;
  border-radius: 3px;
  overflow: hidden;
}

.category-bar-part {
  height: 100%;
}

.category-row {
  cursor: pointer;
}

.category-templates {
  background: rgba(var(--v-theme-on-surface), 0.04);
}
</style>
