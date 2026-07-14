/**
 * Time zones offered in settings: UTC plus the IANA zones covering Russia's
 * eleven offsets (UTC+2..UTC+12), which is the fleet this dashboard is aimed at.
 *
 * Zones are listed by IANA id rather than a translated city name: the ids are
 * what PostgreSQL itself (timezone GUC, AT TIME ZONE) and the server logs use, so
 * an operator can match them without a mental lookup table — and they need no
 * translation. Offsets are not hardcoded; they are derived from the runtime's
 * tz database, so a future change to Russian zones cannot leave a stale label.
 */
export const TIME_ZONES = [
  'UTC',
  'Europe/Kaliningrad', // MSK−1
  'Europe/Moscow', // MSK
  'Europe/Samara', // MSK+1
  'Asia/Yekaterinburg', // MSK+2
  'Asia/Omsk', // MSK+3
  'Asia/Novosibirsk', // MSK+4
  'Asia/Krasnoyarsk', // MSK+4
  'Asia/Irkutsk', // MSK+5
  'Asia/Yakutsk', // MSK+6
  'Asia/Vladivostok', // MSK+7
  'Asia/Magadan', // MSK+8
  'Asia/Kamchatka', // MSK+9
]

const validity = new Map<string, boolean>()

/**
 * Whether the runtime knows this zone. The setting is persisted in localStorage,
 * so it can outlive this list (a hand-edited value, or a zone dropped in a later
 * release) — and an unknown zone makes Intl throw RangeError, which would take
 * down every table that renders a timestamp. Callers check first and fall back to
 * local time. Results are cached: this runs on each formatted cell.
 */
export function isValidTimeZone(timeZone: string): boolean {
  const cached = validity.get(timeZone)
  if (cached !== undefined) return cached

  let ok = true
  try {
    new Intl.DateTimeFormat('en-US', { timeZone })
  } catch {
    ok = false
  }

  validity.set(timeZone, ok)

  return ok
}

/**
 * Short offset label for a zone ("GMT+3"), read from the tz database. Returns an
 * empty string if the runtime does not know the zone, so a caller can fall back
 * to showing the bare id instead of a broken label.
 */
export function tzOffsetLabel(timeZone: string, at: Date = new Date()): string {
  try {
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone,
      timeZoneName: 'shortOffset',
    }).formatToParts(at)

    return parts.find((p) => p.type === 'timeZoneName')?.value ?? ''
  } catch {
    return ''
  }
}

/** "Europe/Moscow (GMT+3)" — the id an operator recognises, plus its current offset. */
export function tzTitle(timeZone: string): string {
  const offset = tzOffsetLabel(timeZone)

  return offset ? `${timeZone} (${offset})` : timeZone
}

/**
 * Suffix appended to a rendered timestamp so a reader always knows which zone it
 * is in. UTC keeps its own name; other zones show the offset, which is shorter
 * and less ambiguous than an abbreviation (MSK vs MSD, etc.).
 */
export function tzSuffix(timeZone: string): string {
  if (timeZone === 'UTC') return ' UTC'

  const offset = tzOffsetLabel(timeZone)

  return offset ? ` ${offset}` : ''
}
