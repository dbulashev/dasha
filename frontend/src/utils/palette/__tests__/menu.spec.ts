import { describe, expect, it } from 'vitest'

import appSource from '@/App.vue?raw'
import { SECTIONS, type SectionDef } from '@/utils/palette/sections'

// The palette keeps its own copy of the drawer menu; this reads the menu out of
// App.vue so that a change to one without the other fails here.

interface MenuEntry {
  slug: string
  titleKey: string
  groupKey?: string
  icon: string
  /** 'always', 'logs' (the group is shown by hasLogSearchClusters) or the menuVisible slug */
  shownBy: string
  keepQuery?: string[]
}

const attr = (tag: string, name: string) => tag.match(new RegExp(`(?:^|\\s)${name}="([^"]*)"`))?.[1]
const titleKeyOf = (tag: string) => attr(tag, ':title')?.match(/^t\('([^']+)'\)$/)?.[1]

function shownBy(vIf: string | undefined): string {
  if (vIf === undefined) return 'always'
  if (vIf === 'hasLogSearchClusters') return 'logs'
  return vIf.match(/^menuVisible\('([^']+)'\)$/)?.[1] ?? `unknown v-if: ${vIf}`
}

/** link computed name → its slug and the query it carries over besides host and db */
function menuLinks(source: string): Map<string, { slug: string; keepQuery: string[] }> {
  const out = new Map<string, { slug: string; keepQuery: string[] }>()
  const re = /const (\w+Link) = computed\(\(\) => (\{[\s\S]*?\n\}\)|withQuery\([^)]*\)\))/g
  for (const [, name, body] of source.matchAll(re)) {
    const slug = body!.match(/withQuery\("([^"]+)"\)/)?.[1]
    if (!slug) throw new Error(`${name}: no withQuery("<slug>")`)
    const keepQuery = [...body!.matchAll(/route\.query\.(\w+)/g)].map(m => m[1]!)
    out.set(name!, { slug, keepQuery: [...new Set(keepQuery)] })
  }
  return out
}

function parseMenu(source: string): MenuEntry[] {
  const nav = source.match(/<v-list nav>([\s\S]*?)<\/v-list>/)?.[1]
  if (!nav) throw new Error('no <v-list nav> in App.vue')
  const links = menuLinks(source)

  const out: MenuEntry[] = []
  let group: { vIf?: string; titleKey?: string; icon?: string } | null = null
  for (const [tag] of nav.matchAll(/<\/?v-list-(?:group|item)\b[^>]*>/g)) {
    if (tag.startsWith('</v-list-group')) {
      group = null
    } else if (tag.startsWith('<v-list-group')) {
      group = { vIf: attr(tag, 'v-if') }
    } else if (tag.startsWith('<v-list-item') && attr(tag, 'v-bind') === 'props') {
      if (!group) throw new Error(`activator outside a group: ${tag}`)
      group.titleKey = titleKeyOf(tag)
      group.icon = attr(tag, 'prepend-icon')
    } else if (tag.startsWith('<v-list-item')) {
      const to = attr(tag, ':to')
      const link = to ? links.get(to) : undefined
      if (!link) throw new Error(`menu item without a known link: ${tag}`)
      const entry: MenuEntry = {
        slug: link.slug,
        titleKey: titleKeyOf(tag) ?? `no t('…') title: ${tag}`,
        icon: (group ? group.icon : attr(tag, 'prepend-icon')) ?? 'no icon',
        shownBy: shownBy(attr(tag, 'v-if') ?? group?.vIf),
      }
      if (group) entry.groupKey = group.titleKey
      if (link.keepQuery.length) entry.keepQuery = link.keepQuery
      out.push(entry)
    }
  }
  return out
}

function asMenuEntry(s: SectionDef): MenuEntry {
  const entry: MenuEntry = {
    slug: s.slug,
    titleKey: s.titleKey,
    icon: s.icon,
    shownBy: s.rule === 'always' || s.rule === 'logs' ? s.rule : s.slug,
  }
  if (s.groupKey) entry.groupKey = s.groupKey
  if (s.keepQuery) entry.keepQuery = s.keepQuery
  return entry
}

describe('SECTIONS against the App.vue menu', () => {
  it('lists the same items in the same order, with the same titles, icons and conditions', () => {
    expect(SECTIONS.map(asMenuEntry)).toEqual(parseMenu(appSource))
  })
})
