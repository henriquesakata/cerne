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

const IN_PROGRESS = new Set(['candidate', 'approved', 'in_development', 'implemented'])

function featureNode(f: StateResponse['features'][number], at = ''): VaultNode {
  return {
    key: `${at}${f.id}`,
    id: f.id,
    title: f.title,
    kind: 'feature',
    state: f.state,
    children: DOCS.map((doc) => ({
      key: `${at}${f.id}/${doc}`,
      id: f.id,
      doc,
      title: doc,
      kind: 'feature' as const,
      children: [],
    })),
  }
}

// `visiting`: the id of a historical entity the reader is on while the history
// toggle is off — pinned on top so the tree still shows where they are.
export function buildVaultTree(
  state: StateResponse,
  wiki: WikiIndex,
  opts: { history: boolean; visiting?: string },
): VaultNode[] {
  const visible = <T extends { state: string }>(kind: VaultKind, xs: T[]) =>
    opts.history ? xs : xs.filter((x) => !isHistorical(kind, x.state))

  const feats = visible('feature', state.features)
  const nodeOf = new Map<string, VaultNode>()
  for (const f of feats) nodeOf.set(f.id, featureNode(f))
  const parentOf = new Map(feats.map((f) => [f.id, f.parent]))
  // A parent chain that loops back on itself has no root: lift to the group root.
  const cyclic = (id: string) => {
    const seen = new Set([id])
    for (let p = parentOf.get(id); p; p = parentOf.get(p)) {
      if (seen.has(p)) return true
      seen.add(p)
    }
    return false
  }
  const roots: VaultNode[] = []
  for (const f of feats) {
    const node = nodeOf.get(f.id)!
    const parent = f.parent && !cyclic(f.id) ? nodeOf.get(f.parent) : undefined
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

  // Visiting a historical entity with the toggle off: pinned on top.
  const visitId = opts.history ? undefined : opts.visiting
  const visited: VaultNode[] = []
  if (visitId) {
    const at = 'visit/'
    const f = state.features.find((x) => x.id === visitId)
    if (f && isHistorical('feature', f.state)) visited.push(featureNode(f, at))
    for (const [kind, xs] of [
      ['decision', state.decisions],
      ['note', state.notes],
      ['runbook', state.runbooks],
      ['reference', state.references],
    ] as const) {
      const e = xs.find((x) => x.id === visitId)
      if (e && isHistorical(kind, e.state))
        visited.push({ key: `${at}${e.id}`, id: e.id, title: e.title, kind, state: e.state, children: [] })
    }
  }
  const inProgress = state.features
    .filter((f) => IN_PROGRESS.has(f.state))
    .map((f) => featureNode(f, 'progress/'))

  return [
    ...(visited.length ? [group('visiting', visited)] : []),
    group('progress', inProgress),
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
      const self = n.kind !== 'group' && (fold(n.title).includes(q) || (n.kind !== 'wiki' && fold(n.id ?? '').includes(q)))
      if (!self && kids.length === 0) continue
      const docs = self ? n.children.filter((c) => c.doc) : []
      out.push({ ...n, children: [...docs, ...kids], count: n.kind === 'group' ? countEntities(kids) : n.count })
    }
    return out
  }
  return walk(nodes)
}
