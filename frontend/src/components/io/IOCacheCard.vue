<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { contextColor, hitRatio, sumBy, sumValues, type MatrixRow } from './types'
import { useThemeStore } from '@/stores/theme'
import { fmtCompact, fmtPct } from '@/utils/format'

const props = defineProps<{ rows: MatrixRow[] }>()

const { t } = useI18n()
const themeStore = useThemeStore()
const isDark = computed(() => themeStore.currentTheme() === 'dark')

const hits = computed(() => sumValues(props.rows, 'hits'))
const reads = computed(() => sumValues(props.rows, 'reads'))
const overall = computed(() => hitRatio(props.rows))

const perContext = computed(() =>
  [...sumBy(props.rows, (r) => r.context)]
    .map(([context, rows]) => ({
      context,
      ratio: hitRatio(rows),
      accesses: sumValues(rows, 'hits') + sumValues(rows, 'reads'),
    }))
    .filter((c) => c.accesses > 0)
    .sort((a, b) => b.accesses - a.accesses),
)

function ratioColor(ratio: number | null): string {
  if (ratio === null) return 'grey'
  if (ratio >= 0.99) return 'success'
  if (ratio >= 0.95) return 'warning'
  return 'error'
}
</script>

<template>
  <v-card class="h-100">
    <v-card-title class="d-flex align-center ga-1">
      <v-icon start icon="mdi-memory" class="flex-shrink-0" />
      <span class="text-wrap">{{ t('io.cache.title') }}</span>
      <v-tooltip :text="t('io.cache.hint')" location="bottom" max-width="360">
        <template #activator="{ props: tip }">
          <v-icon
            v-bind="tip"
            size="small"
            icon="mdi-help-circle-outline"
            class="ms-1 flex-shrink-0 text-medium-emphasis"
          />
        </template>
      </v-tooltip>
    </v-card-title>
    <v-card-text>
      <template v-if="overall !== null">
        <div class="text-h4">{{ fmtPct(overall * 100, 2) }}</div>
        <div class="text-caption text-medium-emphasis">
          {{ t('io.cache.subtitle', { hits: fmtCompact(hits), reads: fmtCompact(reads) }) }}
        </div>
        <v-progress-linear
          :model-value="overall * 100"
          :color="ratioColor(overall)"
          height="8"
          rounded
          class="mb-4 mt-2"
        />

        <div v-for="c in perContext" :key="c.context" class="mb-2">
          <div class="d-flex justify-space-between text-caption">
            <span>
              <v-icon size="x-small" icon="mdi-circle" :color="contextColor(c.context, isDark)" class="me-1" />
              {{ c.context }}
            </span>
            <span>{{ fmtPct((c.ratio ?? 0) * 100, 2) }}</span>
          </div>
          <v-progress-linear
            :model-value="(c.ratio ?? 0) * 100"
            :color="ratioColor(c.ratio)"
            height="6"
            rounded
          />
        </div>
      </template>

      <v-alert v-else type="info" variant="tonal" density="compact">
        {{ t('io.cache.noAccess') }}
      </v-alert>
    </v-card-text>
  </v-card>
</template>
