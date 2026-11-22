import { computed, onScopeDispose, ref, shallowReactive, watch, type Ref } from 'vue'

import { useInstanceInfoStore } from '@/stores/instanceInfo'
import type { HostItem } from '@/utils/clusters'
import {
  assignDigits,
  hostForDigit,
  type DigitAssignment,
  type DigitProblem,
  type HostRoleState,
} from '@/utils/palette/digits'

export const DIGIT_WAIT_MS = 5000

export type DigitNotice =
  | { kind: 'waiting'; digit: number }
  | { kind: 'problem'; digit: number; problem: DigitProblem }
  | { kind: 'no-host'; digit: number }

type RoundResult = 'primary' | 'replica' | 'failed'

/**
 * Roles of the current cluster's hosts for one opening of the palette, and the
 * digits they get. Only answers of this opening or ones still fresh by the
 * store's TTL count: a role remembered from before a failover gives no digit (D6).
 */
export function usePaletteHosts(options: {
  cluster: Ref<string | null>
  hosts: Ref<HostItem[]>
  /** the cluster list is being fetched: the current cluster's hosts may be on the way */
  listLoading?: Ref<boolean>
  onDigit: (host: string) => void
}) {
  const instanceInfoStore = useInstanceInfoStore()

  const results = shallowReactive(new Map<string, RoundResult>())
  const loading = ref(false)
  // Hosts that have not answered by the digit's deadline count as failed for
  // this opening only; their requests go on and land in the store.
  const expired = ref(false)
  let round = 0
  let active = false

  const pendingDigit = ref<number | null>(null)
  const notice = ref<DigitNotice | null>(null)
  let waitTimer: ReturnType<typeof setTimeout> | undefined

  function stateOf(host: string): HostRoleState {
    const result = results.get(host)
    if (result) return result
    const cluster = options.cluster.value
    const fresh = cluster ? instanceInfoStore.get(cluster, host) : null
    if (fresh) return fresh.InRecovery ? 'replica' : 'primary'
    return loading.value && !expired.value ? 'loading' : 'failed'
  }

  const roles = computed(() => options.hosts.value.map(h => ({ host: h.value, state: stateOf(h.value) })))
  const assignment = computed<DigitAssignment>(() => assignDigits(roles.value))

  const digitOf = computed(() => {
    const out = new Map<string, number>()
    if (assignment.value.status === 'ready') {
      for (const [digit, host] of assignment.value.digits) out.set(host, digit)
    }
    return out
  })

  const problem = computed(() =>
    assignment.value.status === 'ready' ? assignment.value.problem : null,
  )

  function start() {
    stop()
    active = true
    poll()
  }

  // The pending digit survives a new round: the hosts it waits for are in it.
  function poll() {
    const id = ++round
    results.clear()
    expired.value = false
    const cluster = options.cluster.value
    const hosts = options.hosts.value
    if (!cluster || !hosts.length) {
      loading.value = false
      return
    }
    loading.value = true
    void Promise.allSettled(
      hosts.map(async h => {
        const info = await instanceInfoStore.ensure(cluster, h.value)
        if (id !== round) return
        results.set(h.value, info ? (info.InRecovery ? 'replica' : 'primary') : 'failed')
      }),
    ).then(() => {
      if (id === round) loading.value = false
    })
  }

  function cancelPending() {
    clearTimeout(waitTimer)
    waitTimer = undefined
    pendingDigit.value = null
    if (notice.value?.kind === 'waiting') notice.value = null
  }

  function stop() {
    active = false
    round++
    cancelPending()
    results.clear()
    loading.value = false
    expired.value = false
    notice.value = null
  }

  const hostsComing = () =>
    !!options.listLoading?.value && !!options.cluster.value && !options.hosts.value.length && !expired.value

  function resolve(digit: number): boolean {
    if (hostsComing()) return false
    const r = hostForDigit(assignment.value, digit)
    if ('pending' in r) return false
    cancelPending()
    if ('host' in r) {
      notice.value = null
      options.onDigit(r.host)
    } else {
      notice.value = r.problem ? { kind: 'problem', digit, problem: r.problem } : { kind: 'no-host', digit }
    }
    return true
  }

  function press(digit: number) {
    cancelPending()
    if (resolve(digit)) return
    pendingDigit.value = digit
    notice.value = { kind: 'waiting', digit }
    waitTimer = setTimeout(() => {
      expired.value = true
      if (pendingDigit.value !== null) resolve(pendingDigit.value)
    }, DIGIT_WAIT_MS)
  }

  // The list may arrive or change while the palette is open (a stale cache is
  // refreshed on opening); sync so that new hosts never read as failed meanwhile.
  watch(
    () => [options.cluster.value, ...options.hosts.value.map(h => h.value)].join('\n'),
    () => {
      if (active) poll()
    },
    { flush: 'sync' },
  )

  watch([assignment, () => options.listLoading?.value], () => {
    if (pendingDigit.value !== null) resolve(pendingDigit.value)
  })

  onScopeDispose(stop)

  return { stateOf, assignment, digitOf, problem, loading, pendingDigit, notice, start, stop, press, cancelPending }
}
