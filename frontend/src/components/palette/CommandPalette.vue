<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useDisplay, useHotkey } from 'vuetify'

import { getClusters } from '@/api/gen/default/default'
import type { Cluster } from '@/api/models'
import { usePaletteHosts, type DigitNotice } from '@/composables/usePaletteHosts'
import { usePaletteContext, usePaletteItems, type PaletteItem } from '@/composables/usePaletteItems'
import { useClustersStore } from '@/stores/clusters'
import { usePaletteRecentStore } from '@/stores/paletteRecent'
import { assertOk } from '@/utils/api'
import type { DigitProblem } from '@/utils/palette/digits'
import { hasAnyModifier, hasModKey, modKeyLabel } from '@/utils/palette/keys'
import { initialIndex } from '@/utils/palette/search'
import PaletteRow from './PaletteRow.vue'

const open = defineModel<boolean>({ default: false })

const { t } = useI18n()
const router = useRouter()
const { xs } = useDisplay()
const clusterStore = useClustersStore()
const recentStore = usePaletteRecentStore()

const query = ref('')
const selected = ref(-1)

// The role prop of v-text-field lands on the field wrapper as well as on the
// input, and nested comboboxes confuse screen readers: set it on the input only.
const field = useTemplateRef<{ $el: HTMLElement }>('field')
watch(field, f => f?.$el.querySelector('input')?.setAttribute('role', 'combobox'))

const ctx = usePaletteContext()
const roles = usePaletteHosts({
  cluster: ctx.cluster,
  hosts: ctx.hosts,
  onDigit: host => {
    const item = items.value.find(i => i.id === `host:${host}`)
    if (item) go(item, false)
  },
})
const { groups, items } = usePaletteItems(ctx, query, roles)

function toggle() {
  open.value = !open.value
}

useHotkey('cmd+k', toggle, { inputs: true })
// useHotkey compares e.key, which is a Cyrillic letter in the Russian layout.
useHotkey('cmd+л', toggle, { inputs: true })

let returnFocus: HTMLElement | null = null

// Synchronous so that the element is read before the dialog takes the focus.
watch(
  open,
  isOpen => {
    if (isOpen) {
      returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
      onOpen()
    } else {
      roles.stop()
    }
  },
  { flush: 'sync', immediate: true },
)

function onOpen() {
  query.value = ''
  selected.value = -1
  recentStore.$hydrate()
  roles.start()
  if (!clusterStore.isCacheValid) void refreshClusters()
}

// The list may be stale; a failure leaves it as it is, the selector reports errors.
async function refreshClusters() {
  try {
    const data = assertOk<Cluster[]>(await getClusters()) ?? []
    clusterStore.setClusters(data)
  } catch {
    // ignored on purpose
  }
}

function onAfterLeave() {
  if (returnFocus?.isConnected) returnFocus.focus()
  returnFocus = null
}

watch(query, () => {
  roles.cancelPending()
  selected.value = initialIndex(items.value)
})

watch(items, list => {
  if (selected.value < 0 || selected.value >= list.length) selected.value = initialIndex(list)
})

const optionId = (i: number) => `palette-opt-${i}`
const activeId = computed(() => (selected.value >= 0 && selected.value < items.value.length ? optionId(selected.value) : undefined))

watch(selected, async () => {
  await nextTick()
  if (activeId.value) document.getElementById(activeId.value)?.scrollIntoView?.({ block: 'nearest' })
})

const indexed = computed(() => {
  let i = 0
  return groups.value.map(g => ({ ...g, rows: g.items.map(item => ({ item, index: i++ })) }))
})

function move(delta: number) {
  const n = items.value.length
  if (!n) return
  selected.value = selected.value < 0 ? (delta > 0 ? 0 : n - 1) : (selected.value + delta + n) % n
}

function go(item: PaletteItem, newTab: boolean) {
  if (item.recent?.cluster) recentStore.record(item.recent.cluster, item.recent.section)
  open.value = false
  if (newTab) window.open(router.resolve(item.to).href, '_blank', 'noopener')
  else void router.push(item.to)
}

function onKeydown(e: KeyboardEvent) {
  if (e.isComposing) return
  switch (e.key) {
    case 'ArrowDown':
    case 'ArrowUp':
      e.preventDefault()
      move(e.key === 'ArrowDown' ? 1 : -1)
      return
    case 'Enter': {
      e.preventDefault()
      const item = items.value[selected.value]
      if (item) go(item, hasModKey(e))
      return
    }
  }
  if (!query.value && !hasAnyModifier(e) && /^[1-9]$/.test(e.key)) {
    e.preventDefault()
    roles.press(Number(e.key))
  }
}

function onPick(item: PaletteItem, e: MouseEvent | KeyboardEvent) {
  go(item, hasModKey(e))
}

function problemText(problem: DigitProblem): string {
  return problem.reason === 'no-primary'
    ? t('palette.noPrimary')
    : t('palette.manyPrimaries', { hosts: problem.hosts.join(', ') })
}

function noticeText(notice: DigitNotice): string {
  switch (notice.kind) {
    case 'waiting':
      return t('palette.waitingRoles')
    case 'problem':
      return problemText(notice.problem)
    case 'no-host':
      return t('palette.noHost', { digit: notice.digit })
  }
}

// A digit the user pressed speaks first; otherwise why 1 is empty, if it is.
const statusLine = computed(() => {
  if (query.value) return null
  const notice = roles.notice.value
  if (notice) return { text: noticeText(notice), waiting: notice.kind === 'waiting' }
  const problem = roles.problem.value
  return problem ? { text: problemText(problem), waiting: false } : null
})

const mod = modKeyLabel()
</script>

<template>
  <v-dialog
    v-model="open"
    :max-width="640"
    :fullscreen="xs"
    scrollable
    :content-class="xs ? undefined : 'palette'"
    @after-leave="onAfterLeave"
  >
    <v-card role="dialog" :aria-label="t('palette.title')">
      <v-card-text class="pa-2 flex-grow-0">
        <v-text-field
          v-model="query"
          autofocus
          hide-details
          density="comfortable"
          variant="solo-filled"
          flat
          prepend-inner-icon="mdi-magnify"
          :placeholder="t('palette.placeholder')"
          ref="field"
          aria-autocomplete="list"
          :aria-expanded="items.length > 0"
          aria-controls="palette-list"
          :aria-activedescendant="activeId"
          :aria-label="t('palette.title')"
          autocomplete="off"
          spellcheck="false"
          @keydown="onKeydown"
        />
      </v-card-text>
      <v-divider />
      <v-card-text class="pa-0">
        <div v-if="statusLine" class="palette-status text-body-2 text-medium-emphasis px-4 pt-2" role="status">
          <v-progress-circular v-if="statusLine.waiting" indeterminate size="14" width="2" class="mr-2" />
          {{ statusLine.text }}
        </div>
        <v-list id="palette-list" role="listbox" :aria-label="t('palette.title')" density="compact">
          <template v-for="g in indexed" :key="g.key">
            <v-list-subheader v-if="g.title" role="presentation">
              {{ g.title }}
              <v-progress-circular
                v-if="g.key === 'hosts' && roles.loading.value"
                indeterminate
                size="12"
                width="2"
                class="ml-2"
              />
            </v-list-subheader>
            <PaletteRow
              v-for="row in g.rows"
              :key="row.item.id"
              :id="optionId(row.index)"
              :item="row.item"
              :selected="row.index === selected"
              @pick="onPick(row.item, $event)"
              @mouseenter="selected = row.index"
            />
          </template>
          <v-list-item v-if="query.trim() && !items.length" role="presentation" class="text-medium-emphasis">
            {{ t('palette.nothingFound') }}
          </v-list-item>
        </v-list>
      </v-card-text>
      <v-divider />
      <div class="palette-footer text-caption text-medium-emphasis px-4 py-2">
        {{ t('palette.hint.move') }} · {{ t('palette.hint.go') }} ·
        {{ t('palette.hint.newTab', { key: `${mod}Enter` }) }} · {{ t('palette.hint.close') }}
      </div>
    </v-card>
  </v-dialog>
</template>

<style>
/* Unscoped: the dialog content is teleported out of this component. */
.v-overlay__content.palette {
  align-self: flex-start;
  margin-top: 12vh;
  max-height: 76vh;
}
</style>
