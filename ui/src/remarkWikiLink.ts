type MdNode = {
  type: string
  value?: string
  url?: string
  children?: MdNode[]
  data?: Record<string, unknown>
}

export type WikiLinkOptions = {
  // resolve gives the href of an id; null = the kind has no page, left as text.
  resolve: (id: string) => string | null
  // known: when given, an id outside it renders as a broken link.
  known?: ReadonlySet<string>
  // tooltip of the broken-link span
  brokenTitle?: string
}

// Same pattern as internal/mdlink/mdlink.go (N-0076): id-form only, optional
// alias, no alternation. [[some title]] is not a wiki-link by design (F-0167).
const WIKI_RX = /\[\[([A-Za-z]+-\d+)(?:\|([^\]\n]*))?\]\]/g

function split(value: string, opts: WikiLinkOptions): MdNode[] | null {
  const out: MdNode[] = []
  let last = 0
  let hit = false
  for (const m of value.matchAll(WIKI_RX)) {
    const id = m[1].toUpperCase()
    const href = opts.resolve(id)
    if (href === null) continue
    hit = true
    if (m.index > last) out.push({ type: 'text', value: value.slice(last, m.index) })
    const label: MdNode[] = [{ type: 'text', value: m[2]?.trim() || id }]
    out.push(
      opts.known && !opts.known.has(id)
        ? { type: 'wikiBroken', children: label, data: { hName: 'span', hProperties: { className: 'wikilink-broken', title: opts.brokenTitle } } }
        : { type: 'link', url: href, children: label, data: { hProperties: { className: 'wikilink' } } },
    )
    last = m.index + m[0].length
  }
  if (!hit) return null
  if (last < value.length) out.push({ type: 'text', value: value.slice(last) })
  return out
}

// remarkWikiLink swaps [[ID]] / [[ID|text]] in text nodes for links. Code
// blocks and inline code are other node types, so they are never touched.
export function remarkWikiLink(opts: WikiLinkOptions) {
  const walk = (node: MdNode) => {
    if (!node.children || node.type === 'link') return
    const next: MdNode[] = []
    for (const c of node.children) {
      const parts = c.type === 'text' && c.value?.includes('[[') ? split(c.value, opts) : null
      if (parts) next.push(...parts)
      else {
        walk(c)
        next.push(c)
      }
    }
    node.children = next
  }
  return (tree: MdNode) => walk(tree)
}
