import type { StateResponse, WikiIndex } from './api'

export type VaultKind = 'feature' | 'decision' | 'note' | 'runbook' | 'reference' | 'wiki'

export type VaultNode = {
  key: string
  id?: string // entity id (wiki pages: `wiki:<rel>`); set on doc nodes too, with doc
  doc?: 'spec' | 'plan' | 'result'
  title: string // group nodes carry the group key (the screen translates it)
  kind: VaultKind | 'group'
  state?: string
  count?: number // groups only: entities inside, nested ones included
  children: VaultNode[]
}

const FEATURE_ENDED = new Set(['done', 'rejected', 'superseded'])
const OTHER_LIVE = new Set(['active', 'current'])

// isHistorical: what the default tree hides. Features end in done/rejected/
// superseded; every other kind is live only while active (reference: current),
// which covers superseded, obsolete and a decision's retired.
export function isHistorical(kind: VaultKind, state: string): boolean {
  if (kind === 'feature') return FEATURE_ENDED.has(state)
  if (kind === 'wiki') return false
  return !OTHER_LIVE.has(state)
}

const DOCS = ['spec', 'plan', 'result'] as const

function countEntities(nodes: VaultNode[]): number {
  let n = 0
  for (const x of nodes) {
    if (x.doc) continue
    n += 1 + countEntities(x.children)
  }
  return n
}

function group(key: string, children: VaultNode[]): VaultNode {
  return { key: `group:${key}`, title: key, kind: 'group', count: countEntities(children), children }
}

export function buildVaultTree(
  state: StateResponse,
  wiki: WikiIndex,
  opts: { history: boolean },
): VaultNode[] {
  const visible = <T extends { state: string }>(kind: VaultKind, xs: T[]) =>
    opts.history ? xs : xs.filter((x) => !isHistorical(kind, x.state))

  const feats = visible('feature', state.features)
  const nodeOf = new Map<string, VaultNode>()
  for (const f of feats) {
    nodeOf.set(f.id, {
      key: f.id,
      id: f.id,
      title: f.title,
      kind: 'feature',
      state: f.state,
      children: DOCS.map((doc) => ({
        key: `${f.id}/${doc}`,
        id: f.id,
        doc,
        title: doc,
        kind: 'feature' as const,
        children: [],
      })),
    })
  }
  const roots: VaultNode[] = []
  for (const f of feats) {
    const node = nodeOf.get(f.id)!
    const parent = f.parent ? nodeOf.get(f.parent) : undefined
    // A parent that is missing or hidden by the history toggle lifts the child
    // to the group root.
    if (parent && parent !== node) parent.children.push(node)
    else roots.push(node)
  }

  const flat = <T extends { id: string; title: string; state: string }>(kind: VaultKind, xs: T[]) =>
    visible(kind, xs).map<VaultNode>((x) => ({
      key: x.id,
      id: x.id,
      title: x.title,
      kind,
      state: x.state,
      children: [],
    }))

  return [
    group('epics', roots),
    group('decisions', flat('decision', state.decisions)),
    group('notes', flat('note', state.notes)),
    group('runbooks', flat('runbook', state.runbooks)),
    group('references', flat('reference', state.references)),
    group(
      'wiki',
      wiki.pages.map((p) => ({
        key: `wiki:${p.rel}`,
        id: `wiki:${p.rel}`,
        title: p.name,
        kind: 'wiki' as const,
        children: [],
      })),
    ),
  ]
}

const fold = (s: string) =>
  s
    .normalize('NFD')
    .replace(/\p{Diacritic}/gu, '')
    .toLowerCase()

// filterVaultTree keeps the nodes whose id or title contains the query
// (case- and accent-insensitive) plus their ancestors. A matching feature keeps
// its spec/plan/result children; its child features stay only if they match.
export function filterVaultTree(nodes: VaultNode[], query: string): VaultNode[] {
  const q = fold(query.trim())
  if (!q) return nodes
  const walk = (list: VaultNode[]): VaultNode[] => {
    const out: VaultNode[] = []
    for (const n of list) {
      const kids = walk(n.children.filter((c) => !c.doc))
      const self = n.kind !== 'group' && (fold(n.title).includes(q) || fold(n.id ?? '').includes(q))
      if (!self && kids.length === 0) continue
      const docs = self ? n.children.filter((c) => c.doc) : []
      out.push({ ...n, children: [...docs, ...kids], count: n.kind === 'group' ? countEntities(kids) : n.count })
    }
    return out
  }
  return walk(nodes)
}
