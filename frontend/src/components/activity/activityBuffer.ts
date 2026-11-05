import type { ActivitySample } from '@/api/models/activitySample'
import type { ActivityQueryKeyKind } from '@/api/models/activityQueryKeyKind'
import { CLASSES, CLASS_INDEX } from './classes'

export type Dimension = 'query' | 'session' | 'database' | 'user' | 'application' | 'event' | 'backendType'

export const DIMENSIONS: Dimension[] = ['query', 'session', 'database', 'user', 'application', 'event', 'backendType']

// Values of one dimension are ORed, dimensions are ANDed.
export type Filters = Partial<Record<Dimension, Set<string>>>

export interface Range {
  from: number // ms, inclusive
  to: number // ms, exclusive
}

export interface AggregateRow {
  value: string
  count: number
  aas: number
  sharePct: number
  byClass: number[] // session counts per class, CLASSES order
}

export interface QueryInfo {
  kind: ActivityQueryKeyKind
  queryId: string
  text: string
}

export interface SessionInfo {
  pid: number
  user: string
  database: string
  application: string
  clientAddr: string
  state: string
  lastSeen: number
}

export interface ChartSeries {
  key: string
  data: (number | null)[]
}

export interface ChartData {
  stepMs: number
  starts: number[]
  series: ChartSeries[]
}

const OVERFLOW = '…'
// A parallel worker runs its leader's statement, so it counts as client load.
const STATEMENT_BACKENDS = new Set(['client backend', 'parallel worker'])
const MAX_U16 = 0xffff
const MAX_U32 = 0xffffffff
const BAR_STEPS_SEC = [1, 2, 5, 10, 15, 30, 60]
const MAX_BARS = 300
export const OTHER_EVENTS = '\u0000other'
// Row of background processes whose value is empty: no statement, database, user or application.
export const BACKGROUND = '\u0000background'

// Ids are reused once released. Strings past `max` share the id `max`, which is never released.
class Dict {
  private strings: (string | undefined)[] = []
  private refs: number[] = []
  private ids = new Map<string, number>()
  private free: number[] = []

  constructor(private readonly max: number) {}

  intern(s: string): number {
    let id = this.ids.get(s)
    if (id === undefined) {
      if (this.free.length) {
        id = this.free.pop()!
      } else if (this.strings.length < this.max) {
        id = this.strings.length
      } else {
        this.strings[this.max] = OVERFLOW
        return this.max
      }
      this.strings[id] = s
      this.refs[id] = 0
      this.ids.set(s, id)
    }
    this.refs[id]++
    return id
  }

  // Returns true when the string is gone.
  release(id: number): boolean {
    if (id === this.max || --this.refs[id] > 0) return false
    const s = this.strings[id]
    if (s !== undefined) this.ids.delete(s)
    this.strings[id] = undefined
    this.free.push(id)
    return true
  }

  get(id: number): string {
    return this.strings[id] ?? ''
  }

  id(s: string): number | undefined {
    return this.ids.get(s)
  }

  get size(): number {
    return this.ids.size
  }
}

interface Stored {
  at: number
  gap: boolean
  n: number
  cls: Uint8Array
  event: Uint16Array
  db: Uint16Array
  user: Uint16Array
  app: Uint16Array
  btype: Uint16Array
  key: Uint32Array
  pid: Int32Array
  bg: Uint8Array // 1 for a background process
  classCounts: Uint16Array // client sessions only
  bgClassCounts: Uint16Array
}

const EMPTY_U8 = new Uint8Array(0)
const EMPTY_U16 = new Uint16Array(0)
const EMPTY_U32 = new Uint32Array(0)
const EMPTY_I32 = new Int32Array(0)

// Query keys carry a kind prefix (q: query_id, f: fingerprint, t: text hash) that the UI does not show.
export function queryKeyLabel(key: string): string {
  return key.replace(/^[qft]:/, '')
}

export function eventValue(cls: string, event: string): string {
  return `${cls}:${event}`
}

// A bar never spans less than minStepMs, the sampling period.
export function barStepMs(spanMs: number, minStepMs = 0): number {
  for (const s of BAR_STEPS_SEC) {
    if (s * 1000 >= minStepMs && spanMs / (s * 1000) <= MAX_BARS) return s * 1000
  }
  return BAR_STEPS_SEC[BAR_STEPS_SEC.length - 1] * 1000
}

export class ActivityBuffer {
  private samples: Stored[] = []
  private events = new Dict(MAX_U16)
  private dbs = new Dict(MAX_U16)
  private users = new Dict(MAX_U16)
  private apps = new Dict(MAX_U16)
  private btypes = new Dict(MAX_U16)
  private keys = new Dict(MAX_U32)
  private queries = new Map<number, QueryInfo>()
  private sessions = new Map<number, SessionInfo>()
  private clockOffsetMs: number | null = null
  private lastSampledAt: number | null = null
  truncatedSeen = false
  // The buffer always holds background processes; this hides them from every view.
  includeBackground = false

  constructor(public windowMs: number) {}

  get length(): number {
    return this.samples.length
  }

  get first(): number | null {
    return this.samples.length ? this.samples[0].at : null
  }

  get last(): number | null {
    return this.samples.length ? this.samples[this.samples.length - 1].at : null
  }

  clear() {
    this.samples = []
    this.events = new Dict(MAX_U16)
    this.dbs = new Dict(MAX_U16)
    this.users = new Dict(MAX_U16)
    this.apps = new Dict(MAX_U16)
    this.btypes = new Dict(MAX_U16)
    this.keys = new Dict(MAX_U32)
    this.queries.clear()
    this.sessions.clear()
    this.clockOffsetMs = null
    this.lastSampledAt = null
    this.truncatedSeen = false
  }

  // Sessions arrive with background processes included; includeBackground filters them.
  // A sample with the sampled_at of the previous one is the server's cached copy of it: skipped.
  push(sample: ActivitySample, clientNow = Date.now()) {
    const at = Date.parse(sample.sampled_at)
    if (at === this.lastSampledAt) return
    this.lastSampledAt = at
    this.clockOffsetMs = at - clientNow

    const n = sample.sessions.length
    const s: Stored = {
      at,
      gap: false,
      n,
      cls: new Uint8Array(n),
      event: new Uint16Array(n),
      db: new Uint16Array(n),
      user: new Uint16Array(n),
      app: new Uint16Array(n),
      btype: new Uint16Array(n),
      key: new Uint32Array(n),
      pid: new Int32Array(n),
      bg: new Uint8Array(n),
      classCounts: new Uint16Array(CLASSES.length),
      bgClassCounts: new Uint16Array(CLASSES.length),
    }

    sample.sessions.forEach((ss, i) => {
      const c = CLASS_INDEX[ss.class] ?? CLASS_INDEX.Other
      s.cls[i] = c
      if (STATEMENT_BACKENDS.has(ss.backend_type)) {
        s.classCounts[c]++
      } else {
        s.bg[i] = 1
        s.bgClassCounts[c]++
      }
      s.event[i] = this.events.intern(ss.wait_event)
      s.db[i] = this.dbs.intern(ss.database)
      s.user[i] = this.users.intern(ss.user)
      s.app[i] = this.apps.intern(ss.application)
      s.btype[i] = this.btypes.intern(ss.backend_type)
      s.pid[i] = ss.pid

      const k = this.keys.intern(ss.query_key)
      s.key[i] = k
      const q = this.queries.get(k)
      if (!q) {
        this.queries.set(k, { kind: ss.query_key_kind, queryId: ss.query_id, text: ss.text ?? '' })
      } else if (!q.text && ss.text) {
        q.text = ss.text
      }

      this.sessions.set(ss.pid, {
        pid: ss.pid,
        user: ss.user,
        database: ss.database,
        application: ss.application,
        clientAddr: ss.client_addr,
        state: ss.state,
        lastSeen: at,
      })
    })

    if (sample.truncated) this.truncatedSeen = true

    this.append(s)
  }

  // A sample that failed: the bucket holding only gaps is drawn empty.
  // Before the first sample there is no server clock to place it on, and nothing to break.
  pushGap(clientNow = Date.now()) {
    if (this.clockOffsetMs === null) return
    this.append({
      at: clientNow + this.clockOffsetMs,
      gap: true,
      n: 0,
      cls: EMPTY_U8,
      event: EMPTY_U16,
      db: EMPTY_U16,
      user: EMPTY_U16,
      app: EMPTY_U16,
      btype: EMPTY_U16,
      key: EMPTY_U32,
      pid: EMPTY_I32,
      bg: EMPTY_U8,
      classCounts: new Uint16Array(CLASSES.length),
      bgClassCounts: new Uint16Array(CLASSES.length),
    })
  }

  private append(s: Stored) {
    const last = this.last
    if (last !== null && s.at < last) s.at = last
    this.samples.push(s)
    this.evict(s.at - this.windowMs)
  }

  private evict(before: number) {
    let drop = 0
    while (drop < this.samples.length && this.samples[drop].at < before) {
      const s = this.samples[drop]
      for (let i = 0; i < s.n; i++) {
        this.events.release(s.event[i])
        this.dbs.release(s.db[i])
        this.users.release(s.user[i])
        this.apps.release(s.app[i])
        this.btypes.release(s.btype[i])
        if (this.keys.release(s.key[i])) this.queries.delete(s.key[i])
      }
      drop++
    }
    if (!drop) return
    this.samples.splice(0, drop)

    const oldest = this.first ?? Infinity
    for (const [pid, info] of this.sessions) {
      if (info.lastSeen < oldest) this.sessions.delete(pid)
    }
  }

  queryInfo(key: string): QueryInfo | undefined {
    const id = this.keys.id(key)
    return id === undefined ? undefined : this.queries.get(id)
  }

  sessionInfo(pid: number): SessionInfo | undefined {
    return this.sessions.get(pid)
  }

  // Interned strings currently held, for tests.
  dictSizes() {
    return {
      events: this.events.size,
      databases: this.dbs.size,
      users: this.users.size,
      applications: this.apps.size,
      backendTypes: this.btypes.size,
      keys: this.keys.size,
      queries: this.queries.size,
      sessions: this.sessions.size,
    }
  }

  private slice(range: Range): Stored[] {
    return this.samples.filter((s) => s.at >= range.from && s.at < range.to)
  }

  private compile(filters: Filters, hidden: Set<number>) {
    const ids = (dict: Dict, values?: Set<string>): Set<number> | null => {
      if (!values?.size) return null
      const out = new Set<number>()
      for (const v of values) {
        const id = dict.id(v)
        if (id !== undefined) out.add(id)
      }
      return out
    }

    const pids = filters.session?.size ? new Set([...filters.session].map(Number)) : null
    const events = filters.event?.size ? filters.event : null
    const f = {
      key: ids(this.keys, filters.query),
      db: ids(this.dbs, filters.database),
      user: ids(this.users, filters.user),
      app: ids(this.apps, filters.application),
      btype: ids(this.btypes, filters.backendType),
    }
    const active = hidden.size > 0 || !!pids || !!events || Object.values(f).some((v) => v !== null)

    const background = this.includeBackground
    const match = (s: Stored, i: number): boolean => {
      if (!background && s.bg[i]) return false
      if (hidden.has(s.cls[i])) return false
      if (f.key && !f.key.has(s.key[i])) return false
      if (f.db && !f.db.has(s.db[i])) return false
      if (f.user && !f.user.has(s.user[i])) return false
      if (f.app && !f.app.has(s.app[i])) return false
      if (f.btype && !f.btype.has(s.btype[i])) return false
      if (pids && !pids.has(s.pid[i])) return false
      if (events && !events.has(eventValue(CLASSES[s.cls[i]], this.events.get(s.event[i])))) return false
      return true
    }

    return { active, match }
  }

  private orBackground(value: string, s: Stored, i: number): string {
    return value === '' && s.bg[i] ? BACKGROUND : value
  }

  private dimensionValue(dim: Dimension, s: Stored, i: number): string {
    switch (dim) {
      case 'query':
        return this.orBackground(this.keys.get(s.key[i]), s, i)
      case 'session':
        return String(s.pid[i])
      case 'database':
        return this.orBackground(this.dbs.get(s.db[i]), s, i)
      case 'user':
        return this.orBackground(this.users.get(s.user[i]), s, i)
      case 'application':
        return this.orBackground(this.apps.get(s.app[i]), s, i)
      case 'event':
        return eventValue(CLASSES[s.cls[i]], this.events.get(s.event[i]))
      case 'backendType':
        return this.btypes.get(s.btype[i])
    }
  }

  aggregate(range: Range, filters: Filters, dim: Dimension, hidden = new Set<number>()): AggregateRow[] {
    const samples = this.slice(range)
    const measured = samples.filter((s) => !s.gap).length
    if (!measured) return []

    const { match } = this.compile(filters, hidden)
    const rows = new Map<string, AggregateRow>()
    let total = 0

    for (const s of samples) {
      for (let i = 0; i < s.n; i++) {
        if (!match(s, i)) continue
        const v = this.dimensionValue(dim, s, i)
        let row = rows.get(v)
        if (!row) {
          row = { value: v, count: 0, aas: 0, sharePct: 0, byClass: new Array(CLASSES.length).fill(0) }
          rows.set(v, row)
        }
        row.count++
        row.byClass[s.cls[i]]++
        total++
      }
    }

    const out = [...rows.values()]
    for (const r of out) {
      r.aas = r.count / measured
      r.sharePct = total ? (r.count * 100) / total : 0
    }

    return out.sort((a, b) => b.count - a.count || a.value.localeCompare(b.value))
  }

  // Bars aligned to the step; a bar without a measured sample is null.
  chart(
    range: Range,
    filters: Filters,
    hidden: Set<number>,
    byEvent: boolean,
    topEvents = 10,
    minStepMs = 0,
  ): ChartData {
    const stepMs = barStepMs(range.to - range.from, minStepMs)
    const first = Math.floor(range.from / stepMs) * stepMs
    const bars = Math.max(1, Math.ceil((range.to - first) / stepMs))
    const starts = Array.from({ length: bars }, (_, i) => first + i * stepMs)
    const measured = new Array<number>(bars).fill(0)

    const samples = this.slice({ from: first, to: range.to })
    const { active, match } = this.compile(filters, hidden)

    const keys: string[] = byEvent
      ? this.aggregate(range, filters, 'event', hidden)
          .slice(0, topEvents)
          .map((r) => r.value)
      : CLASSES.filter((_, c) => !hidden.has(c))
    const index = new Map(keys.map((k, i) => [k, i]))
    const otherIdx = byEvent ? keys.length : -1
    const sums = Array.from({ length: keys.length + (byEvent ? 1 : 0) }, () => new Array<number>(bars).fill(0))

    for (const s of samples) {
      const b = Math.floor((s.at - first) / stepMs)
      if (b < 0 || b >= bars || s.gap) continue
      measured[b]++

      if (!byEvent && !active) {
        CLASSES.forEach((cls, c) => {
          const k = index.get(cls)
          if (k !== undefined) sums[k][b] += s.classCounts[c] + (this.includeBackground ? s.bgClassCounts[c] : 0)
        })
        continue
      }

      for (let i = 0; i < s.n; i++) {
        if (!match(s, i)) continue
        const v = byEvent ? eventValue(CLASSES[s.cls[i]], this.events.get(s.event[i])) : CLASSES[s.cls[i]]
        const k = index.get(v) ?? otherIdx
        if (k >= 0) sums[k][b]++
      }
    }

    const toSeries = (key: string, data: number[]): ChartSeries => ({
      key,
      data: data.map((v, b) => (measured[b] ? v / measured[b] : null)),
    })

    const series = keys.map((k, i) => toSeries(k, sums[i]))
    if (byEvent && sums[otherIdx].some((v) => v > 0)) series.push(toSeries(OTHER_EVENTS, sums[otherIdx]))

    return { stepMs, starts, series }
  }

  // Sessions that spent at least one sample of the range in the given class.
  pidsWithClass(range: Range, cls: string): Set<number> {
    const c = CLASS_INDEX[cls]
    const out = new Set<number>()
    for (const s of this.slice(range)) {
      if (!s.classCounts[c] && !(this.includeBackground && s.bgClassCounts[c])) continue
      for (let i = 0; i < s.n; i++) {
        if (s.cls[i] === c && (this.includeBackground || !s.bg[i])) out.add(s.pid[i])
      }
    }
    return out
  }
}
