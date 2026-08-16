import { beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { flushPromises, mount } from '@vue/test-utils'

import enUS from '@/locales/en_US.json'

const getQueriesReport = vi.fn()
const getDatabaseUsers = vi.fn()

vi.mock('@/api/gen/default/default', () => ({
  getQueriesReport: (...args: unknown[]) => getQueriesReport(...args),
  getDatabaseUsers: (...args: unknown[]) => getDatabaseUsers(...args),
}))

// The deep link lives in the route, and the point of the test is that it can
// change under a component the router keeps mounted.
const route = reactive({
  params: { clustername: 'c1' },
  query: { host: 'host1', db: 'app', queryid: '111' } as Record<string, string>,
})

vi.mock('vue-router', () => ({
  useRoute: () => route,
}))

vi.mock('@/composables/useClusterInfo', () => ({
  useClusterInfo: () => ({
    clusterName: computed(() => 'c1'),
    databaseName: computed(() => 'app'),
    hostName: computed(() => 'host1'),
  }),
}))

vi.mock('@/composables/useQueryScope', () => ({
  useQueryScope: () => ({
    scope: computed(() => 'database'),
    isInstanceScope: computed(() => false),
    hasScopeChoice: computed(() => false),
  }),
}))

const onError = vi.fn()
vi.mock('@/composables/useViewError', () => ({
  useViewError: () => ({ onError }),
}))

import QueryReportSection from '@/components/queries/QueryReportSection.vue'

const vuetify = createVuetify({ components, directives })
const i18n = createI18n({ legacy: false, locale: 'en_US', messages: { en_US: enUS } })

function render() {
  return mount(QueryReportSection, {
    shallow: true,
    global: { plugins: [vuetify, i18n] },
  })
}

function queryIdsAsked() {
  return getQueriesReport.mock.calls.map(
    ([params]) => (params as Record<string, unknown>).queryid,
  )
}

describe('QueryReportSection', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getQueriesReport.mockReset()
    getQueriesReport.mockResolvedValue({ data: [], status: 200 })
    getDatabaseUsers.mockReset()
    getDatabaseUsers.mockResolvedValue({ data: [], status: 200 })
    onError.mockReset()
    route.query = { host: 'host1', db: 'app', queryid: '111' }
  })

  it('follows a second deep link instead of answering the first one twice', async () => {
    const wrapper = render()
    await flushPromises()

    expect(queryIdsAsked()).toEqual(['111'])

    // The router reuses the component when only the query string changes, so the
    // request has to follow the route rather than what setup happened to read.
    route.query = { ...route.query, queryid: '222' }
    await flushPromises()

    expect(queryIdsAsked().at(-1)).toBe('222')

    wrapper.unmount()
  })

  it('moves the filter to the statement the newest link names', async () => {
    const wrapper = render()
    await flushPromises()

    route.query = { ...route.query, queryid: '222' }
    await flushPromises()

    // Requesting one statement while the filter still names another would show
    // an empty report over a full response.
    expect((wrapper.vm as unknown as { search: string }).search).toBe('222')

    wrapper.unmount()
  })

  it('asks for the whole report when no statement is named', async () => {
    route.query = { host: 'host1', db: 'app' }

    const wrapper = render()
    await flushPromises()

    expect(queryIdsAsked()).toEqual([undefined])

    wrapper.unmount()
  })
})
