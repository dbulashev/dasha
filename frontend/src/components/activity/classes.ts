import { ActivityClass } from '@/api/models/activityClass'

// pg_stat_activity.query_id appeared in PostgreSQL 14.
export const ACTIVITY_MIN_VERSION_NUM = 140000

// Stacking order of the chart, bottom first; matches the backend.
export const CLASSES: ActivityClass[] = Object.values(ActivityClass)

export const CLASS_INDEX: Record<string, number> = Object.fromEntries(CLASSES.map((c, i) => [c, i]))

// Rare classes are neutrals of distinct lightness.
const COLORS_LIGHT: Record<ActivityClass, string> = {
  CPU: '#008300',
  IO: '#2a78d6',
  Lock: '#e34948',
  LWLock: '#6250d6',
  BufferPin: '#52514e',
  IPC: '#e87ba4',
  Client: '#eda100',
  ClientIdleTx: '#1baf7a',
  Timeout: '#6da7ec',
  Extension: '#6e6d69',
  Activity: '#c3c2b7',
  Other: '#898781',
}

const COLORS_DARK: Record<ActivityClass, string> = {
  CPU: '#008300',
  IO: '#3987e5',
  Lock: '#e66767',
  LWLock: '#9085e9',
  BufferPin: '#c3c2b7',
  IPC: '#d55181',
  Client: '#c98500',
  ClientIdleTx: '#199e70',
  Timeout: '#6da7ec',
  Extension: '#a3a29c',
  Activity: '#52514e',
  Other: '#898781',
}

const FALLBACK = '#898781'

export const OTHER_COLOR = FALLBACK

// Shades for several events of one class: + mixes toward white, - toward black; the dark theme avoids dark shades.
const SHADES_LIGHT = [0, -0.3, 0.35, -0.5, 0.55, -0.65, 0.7, 0.8]
const SHADES_DARK = [0, 0.3, 0.5, -0.25, 0.65, 0.75, -0.4, 0.85]

function mix(hex: string, t: number): string {
  const target = t > 0 ? 255 : 0
  const a = Math.abs(t)
  const ch = (i: number) => {
    const v = parseInt(hex.slice(i, i + 2), 16)
    return Math.round(v + (target - v) * a)
      .toString(16)
      .padStart(2, '0')
  }
  return `#${ch(1)}${ch(3)}${ch(5)}`
}

export function eventClass(eventKey: string): string {
  const i = eventKey.indexOf(':')
  return i < 0 ? eventKey : eventKey.slice(0, i)
}

// An event wears its class color; the n-th event of the same class on the chart gets the n-th shade.
export function eventColor(eventKey: string, shade: number, dark: boolean): string {
  const steps = dark ? SHADES_DARK : SHADES_LIGHT
  return mix(classColor(eventClass(eventKey), dark), steps[shade % steps.length])
}

// Keeps a key on its slot while it stays on the chart, so a change in rank does not repaint it.
export class SlotAssigner {
  private slots = new Map<string, number>()

  assign(keys: string[]): Map<string, number> {
    const present = new Set(keys)
    for (const k of [...this.slots.keys()]) if (!present.has(k)) this.slots.delete(k)

    const used = new Set(this.slots.values())
    for (const k of keys) {
      if (this.slots.has(k)) continue
      let slot = 0
      while (used.has(slot)) slot++
      this.slots.set(k, slot)
      used.add(slot)
    }

    return new Map(this.slots)
  }
}

// Hatched classes sit too close to another class by hue alone; the stripes tell them apart.
const HATCH_LIGHT: Partial<Record<ActivityClass, string>> = { Timeout: '#1c5cab' }
const HATCH_DARK: Partial<Record<ActivityClass, string>> = { Timeout: '#184f95' }

export function classHatch(cls: string, dark: boolean): string | null {
  return (dark ? HATCH_DARK : HATCH_LIGHT)[cls as ActivityClass] ?? null
}

export function hatchCss(base: string, line: string): string {
  return `repeating-linear-gradient(45deg, ${line} 0 2px, ${base} 2px 6px)`
}

export function classFill(cls: string, dark: boolean): string {
  const base = classColor(cls, dark)
  const line = classHatch(cls, dark)
  return line ? hatchCss(base, line) : base
}

export function classColor(cls: string, dark: boolean): string {
  return (dark ? COLORS_DARK : COLORS_LIGHT)[cls as ActivityClass] ?? FALLBACK
}

// pg_stat_activity.wait_event_type → class, for views that see raw wait types.
export function waitTypeClass(waitType: string): ActivityClass {
  if (!waitType) return ActivityClass.CPU
  return (CLASSES as string[]).includes(waitType) ? (waitType as ActivityClass) : ActivityClass.Other
}
