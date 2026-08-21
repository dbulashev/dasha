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
import { useExcludeUsersStore } from '@/stores/excludeUsers'

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
    predicate: '',
    ddl: 'CREATE INDEX CONCURRENTLY ON public.orders (customer_id, created_at)',
    weight_pct: 31.4,
    covered_queries: [
      {
        // Beyond Number.MAX_SAFE_INTEGER: a queryid must survive as a string.
        query_ids: ['-5881493265671377279'],
        query_id_by_host: {
          host1: '-5881493265671377279',
          host2: '-5881493265671377279',
        },
        fingerprint: 'abc',
        query: 'SELECT 1',
        weight_pct: 31.4,
        calls: 10,
        hosts: ['host1', 'host2'],
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
      hosts: ['host1', 'host2'],
      hosts_without_stats: [],
    },
    unreachable_hosts: [],
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
      stubs: {
        RouterLink: {
          props: ['to'],
          template: '<a :data-to="JSON.stringify(to)"><slot /></a>',
        },
      },
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
    // The heuristic caveat lives in the disclaimer, not on every row: in step 1
    // planner_checked is false everywhere, so a per-row chip says nothing.
    expect(wrapper.text()).toContain(enUS.indexes.advisor.disclaimer)
  })

  it('shows the predicate of a partial candidate next to its key', async () => {
    resolves(
      report({
        candidates: [candidate({ columns: ['tenant_id'], predicate: '"processed_at" IS NULL' })],
        total: 1,
      }),
    )
    const wrapper = await render()

    expect(wrapper.text()).toContain('(tenant_id)')
    expect(wrapper.text()).toContain('WHERE "processed_at" IS NULL')
  })

  it('names the existing indexes a similar_index warning points at', async () => {
    resolves(
      report({
        candidates: [
          candidate({ warnings: [{ code: 'similar_index', names: ['orders_status_tenant_idx'] }] }),
        ],
        total: 1,
      }),
    )
    const wrapper = await render()

    await wrapper.find('tbody tr button').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('orders_status_tenant_idx')
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

  // The count stays visible whatever is folded — hiding it is what would let an
  // empty candidate list read as a clean bill of health (FR-4.8).
  it('shows the unparsed count without being expanded', async () => {
    resolves(
      report({
        not_parsed: [{ reason_code: 'truncated', count: 7 }],
        summary: { ...report().summary, not_parsed_count: 7 },
      }),
    )
    const wrapper = await render()

    expect(wrapper.text()).toContain('7')
    // The per-reason breakdown is evidence for that count, not the headline.
    expect(wrapper.text()).not.toContain(enUS.indexes.advisor.notParsed.truncated)

    await openDetails(wrapper)
    expect(wrapper.text()).toContain(enUS.indexes.advisor.notParsed.truncated)
  })

  it('raises a warning only when the unread statements are a real gap', async () => {
    resolves(
      report({
        not_parsed: [{ reason_code: 'truncated', count: 7 }],
        summary: { ...report().summary, not_parsed_count: 7 },
      }),
    )
    const wrapper = await render()

    expect(wrapper.find('.v-alert.text-warning').exists()).toBe(true)
  })

  it('does not raise an alarm when the unread statements are only monitoring', async () => {
    resolves(
      report({
        not_parsed: [{ reason_code: 'system_relation', count: 26 }],
        summary: { ...report().summary, not_parsed_count: 26 },
      }),
    )
    const wrapper = await render()

    // Dasha's own catalog queries are an outcome, not a gap: the count is still
    // stated, but nothing here is worth a warning.
    expect(wrapper.text()).toContain('26')
    expect(wrapper.find('.v-alert.text-warning').exists()).toBe(false)

    await openDetails(wrapper)
    expect(wrapper.text()).toContain(enUS.indexes.advisor.notParsed.system_relation)
  })

  it('falls back to the bare code when the backend grows a warning this build does not know', async () => {
    resolves(
      report({
        candidates: [candidate({ warnings: [{ code: 'brand_new_code' as never }] })],
        total: 1,
      }),
    )
    const wrapper = await render()

    expect(wrapper.text()).toContain('orders') // the row still rendered

    // Warnings live only in the expanded row now — the column was dropped as a
    // duplicate of what the expansion already spells out.
    await wrapper.find('tbody tr button').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('brand_new_code')
  })

  it('hides itself when the advisor is disabled rather than reporting an error', async () => {
    getIndexesAdvisor.mockResolvedValue({ data: { message: 'not found' }, status: 404 })
    const wrapper = await render()

    expect(wrapper.find('.v-card').exists()).toBe(false)
    expect(onError).not.toHaveBeenCalled()
  })

  it('asks for the whole cluster, not the selected host', async () => {
    resolves(report())
    await render()

    // An instance in the request would silently narrow the workload back to one
    // host, which is the bug this endpoint exists to avoid.
    const params = getIndexesAdvisor.mock.calls[0][0] as Record<string, unknown>
    expect(params.cluster_name).toBe('c1')
    expect(params.database).toBe('app')
    expect(params).not.toHaveProperty('instance')
  })

  // The list is shared with the query report, so it changes from another page —
  // and it decides which statements the ranking ever sees.
  it('reloads when the excluded users change while the section is open', async () => {
    resolves(report())
    await render()

    const first = getIndexesAdvisor.mock.calls[0][0] as Record<string, unknown>
    expect(first.exclude_users).toBeUndefined()

    useExcludeUsersStore().setExcludeUsers('c1', ['telemetry'])
    await flushPromises()

    const last = getIndexesAdvisor.mock.calls.at(-1)![0] as Record<string, unknown>
    expect(last.exclude_users).toEqual(['telemetry'])
  })

  it('names the hosts a candidate list was built from', async () => {
    resolves(report({ candidates: [candidate()], total: 1 }))
    const wrapper = await render()

    expect(wrapper.text()).toContain('host1, host2')
  })

  it('reports a host it could not read instead of returning a shorter list in silence', async () => {
    resolves(report({ unreachable_hosts: ['replica-2'] }))
    const wrapper = await render()

    expect(wrapper.text()).toContain('replica-2')
    expect(wrapper.text()).toContain(enUS.indexes.advisor.unreachable.split('{hosts}')[0].trim())
  })

  it('separates a host without pg_stat_statements from one that did not answer', async () => {
    resolves(
      report({
        summary: { ...report().summary, hosts_without_stats: ['replica-3'] },
      }),
    )
    const wrapper = await render()

    expect(wrapper.text()).toContain('replica-3')
    // Not the same statement as unreachable: the host is up, its load is invisible.
    expect(wrapper.text()).not.toContain(enUS.indexes.advisor.unreachable.split('{hosts}')[0].trim())
  })

  it('shows which hosts a covered statement actually runs on', async () => {
    resolves(report({ candidates: [candidate()], total: 1 }))
    const wrapper = await render()

    await wrapper.find('tbody tr button').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('host2')
  })

  async function openDetails(wrapper: Awaited<ReturnType<typeof render>>) {
    const link = wrapper.findAll('a').find(a => a.text() === enUS.indexes.advisor.details)
    if (!link) throw new Error('details toggle not rendered')
    await link.trigger('click')
    await flushPromises()
  }

  // Helper: the covered statements live in the expanded row, behind the only
  // button a collapsed row has.
  async function expandedLinks(wrapper: Awaited<ReturnType<typeof render>>) {
    await wrapper.find('tbody tr button').trigger('click')
    await flushPromises()

    return wrapper.findAll('a[data-to]').map(a => a.attributes('data-to') ?? '')
  }

  it('opens the query report on a host the statement actually runs on', async () => {
    // The statement runs nowhere near the selected host1: linking there would ask
    // an instance whose pg_stat_statements has never seen this queryid.
    const c = candidate()
    c.covered_queries[0].hosts = ['replica-9']
    c.covered_queries[0].query_id_by_host = { 'replica-9': '-5881493265671377279' }
    resolves(report({ candidates: [c], total: 1 }))

    const links = await expandedLinks(await render())
    const link = links.find(l => l.includes('-5881493265671377279'))

    expect(link).toBeDefined()
    expect(link).toContain('replica-9')
    expect(link).not.toContain('host1')
  })

  it('keeps the selected host when the statement runs there too', async () => {
    resolves(report({ candidates: [candidate()], total: 1 }))

    const links = await expandedLinks(await render())
    const link = links.find(l => l.includes('-5881493265671377279'))

    // host1 is among the statement's hosts, so switching the user to host2 would
    // be a surprise with nothing to gain.
    expect(link).toContain('host1')
  })

  // Hosts and identifiers are folded into two independent lists, so the first of
  // one and the first of the other can name a pair no instance ever reported —
  // and the query report is per-instance, so the link would come back empty.
  it('links the queryid the chosen host actually carries', async () => {
    const c = candidate()
    c.covered_queries[0].query_ids = ['111', '222']
    c.covered_queries[0].hosts = ['replica-9', 'host1']
    c.covered_queries[0].query_id_by_host = { 'replica-9': '111', host1: '222' }
    resolves(report({ candidates: [c], total: 1 }))

    const links = await expandedLinks(await render())
    const link = links.find(l => l.includes('query-report'))

    expect(link).toBeDefined()
    // host1 is selected, and on host1 the statement is 222 — not the 111 that
    // heads the identifier list.
    expect(link).toContain('host1')
    expect(link).toContain('222')
    expect(link).not.toContain('111')
  })

  it('states that pg_stat_statements is missing instead of claiming a clean database', async () => {
    resolves(report({ summary: { ...report().summary, pgss_available: false } }))
    const wrapper = await render()

    expect(wrapper.text()).toContain(enUS.indexes.advisor.pgssUnavailable)
    // "No candidates" would be a verdict on a workload that was never read.
    expect(wrapper.text()).not.toContain(enUS.indexes.advisor.noCandidates)
  })
})
