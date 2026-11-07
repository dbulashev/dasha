import hljs from 'highlight.js/lib/core'
import json from 'highlight.js/lib/languages/json'
import pgsql from 'highlight.js/lib/languages/pgsql'

hljs.registerLanguage('pgsql', pgsql)
hljs.registerLanguage('json', json)

export const SQL_PREVIEW_MAX = 100

export function highlightSql(sql: string): string {
  return hljs.highlight(sql, { language: 'pgsql' }).value
}

export function highlightJson(text: string): string {
  return hljs.highlight(text, { language: 'json' }).value
}

/**
 * Lay out a JSON document over several lines; anything that does not parse is
 * returned untouched, which is what a plan in the text format is.
 */
export function prettyJson(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}

export function isJson(text: string): boolean {
  try {
    JSON.parse(text)

    return true
  } catch {
    return false
  }
}

const LEADING_NOISE = /^(?:\s|--[^\n]*(?:\n|$)|\/\*(?:[^*]|\*(?!\/))*\*\/|\()*/
const TRAILING_NOISE = /^(?:\s|--[^\n]*(?:\n|$)|\/\*(?:[^*]|\*(?!\/))*\*\/|;)*$/
const OPTIMIZABLE = /^(?:select|insert|update|delete|merge|with|values|table)\b/i
const QUOTED_OR_COMMENT = /'(?:[^']|'')*'|"(?:[^"]|"")*"|--[^\n]*|\/\*[\s\S]*?\*\//g
// SELECT ... INTO creates a table and is a utility statement.
const SELECT_INTO = /^select\b[\s\S]*\binto\b/i

// Utility statements reach pg_stat_statements only under track_utility.
// A multi-statement string is not classified: its query_id may belong to any of them.
// Neither is text cut off inside its leading comment.
export function isUtilityStatement(sql: string): boolean {
  const s = sql.replace(LEADING_NOISE, '')
  if (s === '' || s.startsWith('/*')) return false
  const semi = s.indexOf(';')
  if (semi >= 0 && !TRAILING_NOISE.test(s.slice(semi))) return false
  if (SELECT_INTO.test(s.replace(QUOTED_OR_COMMENT, ' '))) return true
  return !OPTIMIZABLE.test(s)
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
