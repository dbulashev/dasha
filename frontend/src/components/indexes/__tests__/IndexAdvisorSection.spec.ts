import { beforeEach, describe, expect, it, vi } from 'vitest'
import { computed } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { flushPromises, mount } from '@vue/test-utils'

import enUS from '@/locales/en_US.json'
import type { IndexAdvisorCandidate, IndexAdvisorReport } from '@/api/models/index'

const getIndexesAdvisor = vi.fn()

vi.mock('@/api/gen/default/default', () => ({
  getIndexesAdvisor: (...args: unknown[]) => getIndexesAdvisor(...args),
}))

vi.mock('@/composables/useClusterInfo', () => ({
  useClusterInfo: () => ({
    clusterName: computed(() => 'c1'),
    databaseName: computed(() => 'app'),
    hostName: computed(() => 'host1'),
  }),
}))

vi.mock('@/composables/useDescribeLink', () => ({
  useDescribeLink: () => ({ describeLink: () => '/table-describe/c1' }),
}))

const onError = vi.fn()
vi.mock('@/composables/useViewError', () => ({
  useViewError: () => ({ onError }),
}))

import IndexAdvisorSection from '@/components/indexes/IndexAdvisorSection.vue'

// VDataTable measures itself through ResizeObserver, which jsdom does not have.
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
} as unknown as typeof ResizeObserver

const vuetify = createVuetify({ components, directives })
const i18n = createI18n({ legacy: false, locale: 'en_US', messages: { en_US: enUS } })

function candidate(over: Partial<IndexAdvisorCandidate> = {}): IndexAdvisorCandidate {
  return {
    schema: 'public',
    table: 'orders',
    columns: ['customer_id', 'created_at'],
    ddl: 'CREATE INDEX CONCURRENTLY ON public.orders (customer_id, created_at)',
    weight_pct: 31.4,
    covered_queries: [
      {
        // Beyond Number.MAX_SAFE_INTEGER: a queryid must survive as a string.
        query_ids: ['-5881493265671377279'],
        fingerprint: 'abc',
        query: 'SELECT 1',
        weight_pct: 31.4,
        calls: 10,
      },
    ],
    table_rows: 1_000_000,
    writes: { inserted: 1, updated: 2, deleted: 3, seq_scans: 4, idx_scans: 5 },
    warnings: [],
    planner_checked: false,
    ...over,
  }
}

function report(over: Partial<IndexAdvisorReport> = {}): IndexAdvisorReport {
  return {
    candidates: [],
    not_parsed: [],
    summary: {
      pgss_available: true,
      analyzed_queries: 10,
      collapsed_groups: 8,
      not_parsed_count: 0,
      covered_time_pct: 0,
      catalog_truncated: false,
    },
    total: 0,
    duration_ms: 12,
    ...over,
  }
}

function resolves(body: IndexAdvisorReport) {
  getIndexesAdvisor.mockResolvedValue({ data: body, status: 200 })
}

async function render() {
  const wrapper = mount(IndexAdvisorSection, {
    global: {
      plugins: [vuetify, i18n],
      stubs: { RouterLink: { template: '<a><slot /></a>' } },
    },
  })
  await flushPromises()
  return wrapper
}

describe('IndexAdvisorSection', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getIndexesAdvisor.mockReset()
    onError.mockReset()
  })

  it('renders a candidate with its DDL and covered statements', async () => {
    resolves(report({ candidates: [candidate()], total: 1 }))
    const wrapper = await render()

    expect(wrapper.text()).toContain('orders')
    expect(wrapper.text()).toContain('(customer_id, created_at)')
    expect(wrapper.text()).toContain('31.4%')
    // planner_checked=false must stay visible as the heuristic caveat.
    expect(wrapper.text()).toContain(enUS.indexes.advisor.heuristic)
  })

  it('keeps a queryid past Number.MAX_SAFE_INTEGER intact', async () => {
    resolves(report({ candidates: [candidate()], total: 1 }))
    const wrapper = await render()

    // The covered statements live in the expanded row; the toggle is the only
    // button a collapsed row has.
    await wrapper.find('tbody tr button').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('-5881493265671377279')
  })

  it('explains an empty candidate list instead of showing a bare table', async () => {
    resolves(report())
    const wrapper = await render()

    expect(wrapper.text()).toContain(enUS.indexes.advisor.noCandidates)
  })

  it('shows the unparsed workload so an empty list cannot read as "all is well"', async () => {
    resolves(
      report({
        not_parsed: [{ reason_code: 'truncated', count: 7 }],
        summary: { ...report().summary, not_parsed_count: 7 },
      }),
    )
    const wrapper = await render()

    expect(wrapper.text()).toContain(enUS.indexes.advisor.notParsed.truncated)
    expect(wrapper.text()).toContain('7')
  })

  it('does not raise an alarm when the unread statements are only monitoring', async () => {
    resolves(
      report({
        not_parsed: [{ reason_code: 'system_relation', count: 26 }],
        summary: { ...report().summary, not_parsed_count: 26 },
      }),
    )
    const wrapper = await render()

    const alarm = enUS.indexes.advisor.notParsedCount.split('{n}')[1].trim()
    expect(wrapper.text()).toContain(enUS.indexes.advisor.notParsed.system_relation)
    expect(wrapper.text()).not.toContain(alarm)
  })

  it('falls back to the bare code when the backend grows a warning this build does not know', async () => {
    resolves(
      report({
        candidates: [candidate({ warnings: [{ code: 'brand_new_code' as never }] })],
        total: 1,
      }),
    )
    const wrapper = await render()

    expect(wrapper.text()).toContain('brand_new_code')
    expect(wrapper.text()).toContain('orders') // the row still rendered
  })

  it('hides itself when the advisor is disabled rather than reporting an error', async () => {
    getIndexesAdvisor.mockResolvedValue({ data: { message: 'not found' }, status: 404 })
    const wrapper = await render()

    expect(wrapper.find('.v-card').exists()).toBe(false)
    expect(onError).not.toHaveBeenCalled()
  })

  it('states that pg_stat_statements is missing instead of claiming a clean database', async () => {
    resolves(report({ summary: { ...report().summary, pgss_available: false } }))
    const wrapper = await render()

    expect(wrapper.text()).toContain(enUS.indexes.advisor.pgssUnavailable)
    // "No candidates" would be a verdict on a workload that was never read.
    expect(wrapper.text()).not.toContain(enUS.indexes.advisor.noCandidates)
  })
})
