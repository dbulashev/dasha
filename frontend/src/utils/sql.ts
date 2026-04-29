import hljs from 'highlight.js/lib/core'
import pgsql from 'highlight.js/lib/languages/pgsql'

hljs.registerLanguage('pgsql', pgsql)

export const SQL_PREVIEW_MAX = 100

export function highlightSql(sql: string): string {
  return hljs.highlight(sql, { language: 'pgsql' }).value
}

export function truncateSql(sql: string, maxLen = SQL_PREVIEW_MAX): string {
  if (sql.length <= maxLen) return sql
  return sql.substring(0, maxLen) + '…'
}

export function copyToClipboard(text: string) {
  if (navigator.clipboard) {
    navigator.clipboard.writeText(text)
  } else {
    const ta = document.createElement('textarea')
    ta.value = text
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
}
