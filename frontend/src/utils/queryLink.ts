import type { RouteLocationRaw } from 'vue-router'

// Query report of one statement on one host.
export function queryReportLink(
  cluster: string,
  host: string | null,
  queryid: string,
): RouteLocationRaw {
  return {
    name: 'query-report',
    params: { clustername: cluster },
    query: {
      ...(host ? { host } : {}),
      queryid,
    },
  }
}
