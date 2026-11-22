import type { LocationQuery, LocationQueryRaw, RouteLocationNamedRaw } from 'vue-router'

import type { Cluster } from '@/api/models'
import { defaultDb, defaultHost } from '@/utils/clusters'
import { MAIN_SECTION, sectionByRouteName, type SectionDef } from './sections'

export interface Place {
  /** null on pages outside the menu (Fleet) */
  section: SectionDef | null
  cluster: string | null
  query: LocationQuery
}

export function placeOf(route: { name?: unknown; params: Record<string, unknown>; query: LocationQuery }): Place {
  const cluster = route.params.clustername
  return {
    section: sectionByRouteName(route.name),
    cluster: typeof cluster === 'string' && cluster ? cluster : null,
    query: route.query,
  }
}

function to(section: SectionDef, cluster: string | null, query: LocationQueryRaw): RouteLocationNamedRaw {
  return {
    name: section.routeName,
    params: cluster ? { clustername: cluster } : {},
    query,
  }
}

/**
 * The section a target actually opens: an unavailable one falls back to the
 * summary right away (D10), and Fleet, having no section, lands on it too.
 */
export function resolveSection(
  section: SectionDef | null,
  visible: (section: SectionDef) => boolean,
): { section: SectionDef; fallback: boolean } {
  if (!section) return { section: MAIN_SECTION, fallback: false }
  if (visible(section)) return { section, fallback: false }
  return { section: MAIN_SECTION, fallback: true }
}

/** Same place on another host; everything else in the URL stays, as with the selector. */
export function hostLocation(place: Place, host: string, replica: boolean): RouteLocationNamedRaw {
  const section = place.section ?? MAIN_SECTION
  const target = replica && section.rule === 'primaryOnly' ? MAIN_SECTION : section
  return to(target, place.cluster, { ...place.query, host })
}

export function dbLocation(place: Place, db: string): RouteLocationNamedRaw {
  return to(place.section ?? MAIN_SECTION, place.cluster, { ...place.query, db })
}

/** Another section of the current cluster: host and db carry over, as menu links do. */
export function sectionLocation(place: Place, section: SectionDef): RouteLocationNamedRaw {
  const query: Record<string, string> = {}
  for (const key of ['host', 'db', ...(section.keepQuery ?? [])]) {
    const value = place.query[key]
    if (value) query[key] = String(value)
  }
  return to(section, place.cluster, query)
}

/**
 * A section of a given cluster. In the current cluster this is sectionLocation;
 * in another one the host and database are its defaults and nothing else of the
 * current URL is carried over, as on a cluster switch in the selector.
 */
export function clusterSectionLocation(place: Place, cluster: Cluster, section: SectionDef): RouteLocationNamedRaw {
  const name = cluster.name ?? ''
  if (name === place.cluster) return sectionLocation(place, section)
  const host = defaultHost(cluster)
  const db = defaultDb(cluster)
  return to(section, name, {
    ...(host ? { host } : {}),
    ...(db ? { db } : {}),
  })
}
