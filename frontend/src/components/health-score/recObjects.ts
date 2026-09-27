export interface RecObject {
  database?: string
  schema: string
  table: string
  [key: string]: unknown
}

export interface RecObjects {
  objects: RecObject[]
  more: number
}

function isRecObject(v: unknown): v is RecObject {
  if (typeof v !== 'object' || v === null) return false
  const o = v as Record<string, unknown>
  return (
    typeof o.schema === 'string' &&
    typeof o.table === 'string' &&
    (o.database === undefined || typeof o.database === 'string')
  )
}

export function recObjects(context: Record<string, unknown> | null | undefined): RecObjects | null {
  const objects = context?.objects
  if (!Array.isArray(objects)) return null

  const valid = objects.filter(isRecObject)
  if (!valid.length) return null

  const more = typeof context?.more === 'number' && context.more > 0 ? context.more : 0
  return { objects: valid, more }
}

export function recObjectName(o: RecObject): string {
  return [o.database, o.schema, o.table].filter(Boolean).join('.')
}
