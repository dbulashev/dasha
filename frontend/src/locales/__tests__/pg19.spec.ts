import { describe, expect, it } from 'vitest'

import deDE from '@/locales/de_DE.json'
import enUS from '@/locales/en_US.json'
import ruRU from '@/locales/ru_RU.json'

// Namespaces added for PostgreSQL 19: every locale must carry the same keys,
// otherwise the German or Russian UI shows raw key paths.
const NAMESPACES = [
  'common',
  'locks.stats',
  'replication.recovery',
  'replication.slotSync',
  'maintenance.queue',
  'maintenance.summary.startedBy',
  'progress.startedBy',
  'progress.mode',
  'progress.modeHint',
  'progress.vacuumWarning',
  'progress.repack',
  'progress.baseBackupType',
]

function leaves(node: unknown, prefix = ''): string[] {
  if (typeof node !== 'object' || node === null) return [prefix]
  return Object.entries(node).flatMap(([k, v]) => leaves(v, prefix ? `${prefix}.${k}` : k))
}

function at(messages: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((n, k) => (n as Record<string, unknown> | undefined)?.[k], messages)
}

describe('PG 19 locale keys', () => {
  it.each(NAMESPACES)('%s', (ns) => {
    const en = leaves(at(enUS, ns)).sort()
    expect(en.length).toBeGreaterThan(0)
    expect(leaves(at(ruRU, ns)).sort()).toEqual(en)
    expect(leaves(at(deDE, ns)).sort()).toEqual(en)
  })
})
