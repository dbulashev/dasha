/**
 * Role of a host as far as the current palette opening knows it. 'primary' and
 * 'replica' are fresh answers only: a role remembered from before (known()) is
 * 'loading' here, because a digit must not lead to a former primary.
 */
export type HostRoleState = 'primary' | 'replica' | 'loading' | 'failed'

export interface HostRole {
  host: string
  state: HostRoleState
}

export type DigitProblem =
  | { reason: 'no-primary' }
  | { reason: 'many-primaries'; hosts: string[] }

export type DigitAssignment =
  | { status: 'pending' }
  | { status: 'ready'; digits: Map<number, string>; problem: DigitProblem | null }

export const PRIMARY_DIGIT = 1
export const MAX_DIGIT = 9

/** Hosts come in the order of hostItems(); replicas are numbered in that order. */
export function assignDigits(hosts: readonly HostRole[]): DigitAssignment {
  if (hosts.length === 0) return { status: 'ready', digits: new Map(), problem: null }
  if (hosts.length === 1) {
    return { status: 'ready', digits: new Map([[PRIMARY_DIGIT, hosts[0]!.host]]), problem: null }
  }
  if (hosts.some(h => h.state === 'loading')) return { status: 'pending' }

  const digits = new Map<number, string>()
  let problem: DigitProblem | null = null

  const primaries = hosts.filter(h => h.state === 'primary').map(h => h.host)
  if (primaries.length === 1) digits.set(PRIMARY_DIGIT, primaries[0]!)
  else if (primaries.length === 0) problem = { reason: 'no-primary' }
  // Picking one of several primaries at random is worse than not jumping at all.
  else problem = { reason: 'many-primaries', hosts: primaries }

  let next = PRIMARY_DIGIT + 1
  for (const h of hosts) {
    if (next > MAX_DIGIT) break
    if (h.state === 'replica') digits.set(next++, h.host)
  }

  return { status: 'ready', digits, problem }
}

/** Host behind a digit, or why there is none. */
export function hostForDigit(
  assignment: DigitAssignment,
  digit: number,
): { host: string } | { pending: true } | { problem: DigitProblem | null } {
  if (assignment.status === 'pending') return { pending: true }
  const host = assignment.digits.get(digit)
  if (host) return { host }
  return { problem: digit === PRIMARY_DIGIT ? assignment.problem : null }
}
