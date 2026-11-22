import type { Cluster } from '@/api/models'

export interface HostItem {
  /** host_name: the selector value and the `host` URL parameter */
  value: string
  title: string
  /** shown next to the title when it differs from the host name */
  address?: string
}

export function hostItems(cluster: Cluster | null | undefined): HostItem[] {
  const items: HostItem[] = []
  for (const inst of cluster?.instances ?? []) {
    if (!inst.host_name) continue
    const alias = inst.display_name
    items.push(
      alias && alias !== inst.host_name
        ? { value: inst.host_name, title: alias, address: inst.host_name }
        : { value: inst.host_name, title: inst.host_name },
    )
  }
  return items.sort((a, b) => a.title.localeCompare(b.title) || a.value.localeCompare(b.value))
}

// The host and database a cluster switch lands on. Instance and database order is
// the backend's, unsorted: the selector has always picked these, and the palette
// must land on the same place.
export function defaultHost(cluster: Cluster | null | undefined): string | null {
  return cluster?.instances?.find(i => i.host_name)?.host_name ?? null
}

export function defaultDb(cluster: Cluster | null | undefined): string | null {
  return cluster?.databases?.[0] ?? null
}

/** Name of the primary cluster, or null when it is unknown or not in the list. */
export function primaryOf(clusters: Cluster[] | null | undefined, name: string | null | undefined): string | null {
  if (!name) return null
  const primary = clusters?.find(c => c.name === name)?.standby_of
  if (!primary || primary === name) return null
  return clusters?.some(c => c.name === primary) ? primary : null
}

export function standbysOf(clusters: Cluster[] | null | undefined, name: string | null | undefined): string[] {
  if (!name) return []
  return (clusters ?? [])
    .filter(c => c.name && c.name !== name && c.standby_of === name)
    .map(c => c.name as string)
    .sort()
}
