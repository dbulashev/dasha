import { computed, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, type RouteLocationRaw } from 'vue-router'

import type { Cluster } from '@/api/models'
import { useSectionVisibility } from '@/composables/useSectionVisibility'
import type { usePaletteHosts } from '@/composables/usePaletteHosts'
import { useClustersStore } from '@/stores/clusters'
import { useInstanceInfoStore } from '@/stores/instanceInfo'
import { usePaletteRecentStore } from '@/stores/paletteRecent'
import { defaultHost, hostItems, type HostItem } from '@/utils/clusters'
import {
  clusterSectionLocation,
  dbLocation,
  hostLocation,
  placeOf,
  resolveSection,
  sectionLocation,
  type Place,
} from '@/utils/palette/location'
import {
  PAIR_SEPARATOR,
  prepareField,
  prepareHost,
  search,
  type Range,
  type SearchResult,
} from '@/utils/palette/search'
import { buildSectionIndex, MAIN_SECTION, type SectionDef } from '@/utils/palette/sections'

export type ItemKind = 'host' | 'section' | 'cluster' | 'cluster-section' | 'db' | 'recent'

export interface PaletteItem {
  id: string
  kind: ItemKind
  icon: string
  title: string
  titleMarks: Range[]
  subtitle: string[]
  digit?: number
  current: boolean
  fallback: boolean
  to: RouteLocationRaw
  /** what goes into the recent list: the section actually opened */
  recent: { cluster: string; section: string } | null
}

export interface PaletteGroup {
  key: string
  title: string | null
  items: PaletteItem[]
}

const CLUSTER_ICON = 'mdi-database-outline'
const HOST_ICON = 'mdi-server'
const DB_ICON = 'mdi-database'
const RECENT_ICON = 'mdi-history'

export function usePaletteContext() {
  const route = useRoute()
  const clusterStore = useClustersStore()

  // Fleet keeps a cluster in its URL, but there is no section to open it in.
  const fleet = computed(() => route.name === 'fleet')
  const place = computed<Place>(() => {
    const p = placeOf(route)
    return fleet.value ? { ...p, cluster: null } : p
  })
  const cluster = computed(() => place.value.cluster)
  const clusterObj = computed<Cluster | null>(
    () => clusterStore.clusterList?.find(c => c.name === cluster.value) ?? null,
  )
  const host = computed(() => (cluster.value && route.query.host ? String(route.query.host) : null))
  const db = computed(() => (cluster.value && route.query.db ? String(route.query.db) : null))
  const hosts = computed<HostItem[]>(() => hostItems(clusterObj.value))

  return { place, cluster, clusterObj, host, db, hosts }
}

export type PaletteContext = ReturnType<typeof usePaletteContext>

export function usePaletteItems(
  ctx: PaletteContext,
  query: Ref<string>,
  roles: ReturnType<typeof usePaletteHosts>,
) {
  const { t, locale } = useI18n()
  const clusterStore = useClustersStore()
  const instanceInfoStore = useInstanceInfoStore()
  const recentStore = usePaletteRecentStore()

  const { visibilityFor, visibleByName } = useSectionVisibility({
    cluster: computed(() => ctx.cluster.value ?? ''),
    host: computed(() => ctx.host.value ?? ''),
  })

  const sectionIndex = computed(() => {
    void locale.value
    return buildSectionIndex((key, l) => (l ? t(key, {}, { locale: l }) : t(key)))
  })
  const titleOf = (section: SectionDef) =>
    sectionIndex.value.find(e => e.section === section)?.title ?? section.slug

  const clusterList = computed(() => clusterStore.clusterList ?? [])
  const clusterByName = computed(() => new Map(clusterList.value.map(c => [c.name ?? '', c])))
  const preparedClusters = computed(() =>
    clusterList.value.filter(c => c.name).map(c => prepareField(c.name!)),
  )
  const preparedHosts = computed(() => ctx.hosts.value.map(prepareHost))
  const preparedDbs = computed(() => (ctx.clusterObj.value?.databases ?? []).map(prepareField))
  const visibleIn = computed(() => visibleByName(clusterList.value))

  const recentPairs = computed(() =>
    recentStore.entries.map(e => ({ cluster: e.cluster, section: e.section.slug })),
  )

  function clusterOf(name: string): Cluster {
    return clusterByName.value.get(name) ?? { name }
  }

  function visibilityOf(name: string) {
    const current = name === ctx.cluster.value
    return visibilityFor({ cluster: clusterOf(name), host: current ? ctx.host.value : null, current })
  }

  function fallbackNote(section: SectionDef): string {
    return t('palette.fallbackMain', { section: titleOf(section), main: titleOf(MAIN_SECTION) })
  }

  function defaultHostNote(name: string): string[] {
    if (name === ctx.cluster.value) return []
    const h = defaultHost(clusterOf(name))
    return h ? [t('palette.defaultHost', { host: h })] : []
  }

  // A section of a cluster, falling back to the summary where it is not
  // available (D10). Recent entries and cluster results go through here.
  function sectionItem(
    base: Pick<PaletteItem, 'id' | 'kind' | 'icon' | 'title' | 'titleMarks'>,
    name: string,
    wanted: SectionDef | null,
    extra: string[] = [],
  ): PaletteItem {
    const { section, fallback } = resolveSection(wanted, visibilityOf(name))
    // A cluster result does not name its section, so it says where it lands.
    const landing = fallback ? [fallbackNote(wanted!)] : base.kind === 'cluster' ? [`→ ${titleOf(section)}`] : []
    return {
      ...base,
      subtitle: [...landing, ...defaultHostNote(name), ...extra],
      current: name === ctx.cluster.value && section === ctx.place.value.section,
      fallback,
      to: clusterSectionLocation(ctx.place.value, clusterOf(name), section),
      recent: { cluster: name, section: section.slug },
    }
  }

  function roleLabel(host: string): string | null {
    const cluster = ctx.cluster.value
    switch (roles.stateOf(host)) {
      case 'primary':
        return t('hostRole.primary')
      case 'replica':
        return t('hostRole.replica')
      case 'failed':
        return t('hostRole.unknown')
      case 'loading': {
        const known = cluster ? instanceInfoStore.role(cluster, host) : null
        return known ? t(`hostRole.${known}`) : null
      }
    }
  }

  function hostItem(host: HostItem, titleMarks: Range[], current: boolean, synonym?: string): PaletteItem {
    const cluster = ctx.cluster.value!
    const place = ctx.place.value
    const replica = instanceInfoStore.isReplica(cluster, host.value)
    const section = place.section ?? MAIN_SECTION
    const fallback = replica && section.rule === 'primaryOnly'
    const role = roleLabel(host.value)
    const digit = roles.digitOf.value.get(host.value)
    return {
      id: `host:${host.value}`,
      kind: 'host',
      icon: HOST_ICON,
      title: host.title,
      titleMarks,
      subtitle: [
        ...(host.address && host.address !== synonym ? [host.address] : []),
        ...(synonym ? [synonym] : []),
        ...(role ? [role] : []),
        ...(fallback ? [fallbackNote(section)] : []),
      ],
      ...(digit !== undefined ? { digit } : {}),
      current,
      fallback,
      to: hostLocation(place, host.value, replica),
      recent: { cluster, section: (fallback ? MAIN_SECTION : section).slug },
    }
  }

  function fromResult(r: SearchResult): PaletteItem {
    const synonym = r.synonym ? [r.synonym] : []
    const place = ctx.place.value
    switch (r.kind) {
      case 'section':
        return {
          id: `section:${r.section!.slug}`,
          kind: 'section',
          icon: r.section!.icon,
          title: r.title,
          titleMarks: r.titleMarks,
          subtitle: synonym,
          current: r.current,
          fallback: false,
          to: sectionLocation(place, r.section!),
          recent: { cluster: ctx.cluster.value!, section: r.section!.slug },
        }
      case 'cluster':
        return sectionItem(
          { id: `cluster:${r.cluster}`, kind: 'cluster', icon: CLUSTER_ICON, title: r.title, titleMarks: r.titleMarks },
          r.cluster!,
          place.section,
        )
      case 'cluster-section':
        return {
          ...sectionItem(
            {
              id: `cs:${r.cluster}:${r.section!.slug}`,
              kind: 'cluster-section',
              icon: r.section!.icon,
              title: r.title,
              titleMarks: r.titleMarks,
            },
            r.cluster!,
            r.section!,
            synonym,
          ),
          current: r.current,
        }
      case 'host':
        return hostItem(r.host!, r.titleMarks, r.current, r.synonym)
      case 'db':
        return {
          id: `db:${r.db}`,
          kind: 'db',
          icon: DB_ICON,
          title: r.title,
          titleMarks: r.titleMarks,
          subtitle: [t('palette.db')],
          current: r.current,
          fallback: false,
          to: dbLocation(place, r.db!),
          recent: { cluster: ctx.cluster.value!, section: (place.section ?? MAIN_SECTION).slug },
        }
    }
  }

  const results = computed<PaletteItem[]>(() => {
    if (!query.value.trim()) return []
    return search({
      query: query.value,
      clusters: preparedClusters.value,
      sections: sectionIndex.value,
      hosts: preparedHosts.value,
      dbs: preparedDbs.value,
      currentCluster: ctx.cluster.value,
      currentSection: ctx.place.value.section?.slug ?? null,
      currentHost: ctx.host.value,
      currentDb: ctx.db.value,
      visibleIn: visibleIn.value,
      recent: recentPairs.value,
    }).map(fromResult)
  })

  // Primary with 1 first, replicas by their digits, then hosts without one.
  const hostRows = computed<PaletteItem[]>(() => {
    if (!ctx.cluster.value) return []
    return ctx.hosts.value
      .map((h, i) => ({ row: hostItem(h, [], h.value === ctx.host.value), i }))
      .sort((a, b) => (a.row.digit ?? Infinity) - (b.row.digit ?? Infinity) || a.i - b.i)
      .map(({ row }) => row)
  })

  const recentRows = computed<PaletteItem[]>(() =>
    recentStore.entries.map(e =>
      sectionItem(
        {
          id: `recent:${e.cluster}:${e.section.slug}`,
          kind: 'recent',
          icon: RECENT_ICON,
          title: e.cluster + PAIR_SEPARATOR + titleOf(e.section),
          titleMarks: [],
        },
        e.cluster,
        e.section,
      ),
    ),
  )

  const groups = computed<PaletteGroup[]>(() => {
    if (query.value.trim()) return results.value.length ? [{ key: 'results', title: null, items: results.value }] : []
    const out: PaletteGroup[] = []
    if (hostRows.value.length) {
      out.push({ key: 'hosts', title: t('palette.hosts', { cluster: ctx.cluster.value }), items: hostRows.value })
    }
    if (recentRows.value.length) out.push({ key: 'recent', title: t('palette.recent'), items: recentRows.value })
    return out
  })

  const items = computed(() => groups.value.flatMap(g => g.items))

  return { groups, items }
}
