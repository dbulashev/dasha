import { useRoute } from 'vue-router'
import { useClusterInfo } from '@/composables/useClusterInfo'

export function useDescribeLink() {
  const route = useRoute()
  const { hostName, databaseName } = useClusterInfo()

  function describeLink(schema: string, table: string) {
    const cluster = route.params.clustername ?? ''
    const query: Record<string, string> = { schema, table }
    if (hostName.value) query.host = hostName.value
    if (databaseName.value) query.db = databaseName.value
    return { path: `/table-describe/${cluster}`, query }
  }

  function describeLinkFromQualified(qualifiedName: string) {
    const dot = qualifiedName.indexOf('.')
    const schema = dot > 0 ? qualifiedName.substring(0, dot) : 'public'
    const table = dot > 0 ? qualifiedName.substring(dot + 1) : qualifiedName
    return describeLink(schema, table)
  }

  return { describeLink, describeLinkFromQualified }
}
