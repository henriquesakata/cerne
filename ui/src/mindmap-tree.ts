import type { Feature, FeatureState, StateResponse } from './api'

// Projeção pura do /api/state na árvore do mindmap (F-0186):
// raiz (projeto) → módulos (balde "" por último) → épicos/features por parent.
// Determinística — mesma entrada, mesma árvore; nada de rede ou LLM aqui.

export type MindNode = {
  id: string // 'root' | 'module:<nome>' | id da feature
  label: string // nome do projeto | nome do módulo ('' = sem módulo) | título da feature
  kind: 'root' | 'module' | 'feature'
  state?: FeatureState
  // false quando o nó só está na árvore como estrutura (ancestral de um
  // visível que não passa no filtro de estado) — o render esmaece.
  passesFilter?: boolean
  // nº de features descendentes podadas quando este nó está colapsado.
  hiddenDescendants: number
  children: MindNode[]
}

export type MindmapOptions = {
  hideTerminal: boolean
  deliveredOnly: boolean
  collapsed: Set<string>
}

export type MindmapResult = { root: MindNode; featureCount: number }

const TERMINAL: ReadonlySet<string> = new Set(['superseded', 'rejected'])
const DELIVERED: ReadonlySet<string> = new Set(['implemented', 'done'])

// splitSides divide os filhos de 1º nível em dois lados balanceados (F-0187):
// greedy por peso da subárvore VISÍVEL (nós renderizados), maior primeiro no
// lado mais leve (empate de peso → menor id; empate de lado → direita). A ordem
// de exibição original é preservada dentro de cada lado. Determinístico.
export function splitSides(root: MindNode): { right: MindNode[]; left: MindNode[] } {
  const weight = (n: MindNode): number =>
    1 + n.children.reduce((sum, c) => sum + weight(c), 0)
  const order = new Map(root.children.map((n, i) => [n.id, i]))
  const entries = [...root.children]
    .map((n) => ({ n, w: weight(n) }))
    .sort((a, b) => b.w - a.w || a.n.id.localeCompare(b.n.id))
  const right: MindNode[] = []
  const left: MindNode[] = []
  let wr = 0
  let wl = 0
  for (const { n, w } of entries) {
    if (wl < wr) {
      left.push(n)
      wl += w
    } else {
      right.push(n)
      wr += w
    }
  }
  const byOrder = (a: MindNode, b: MindNode) => order.get(a.id)! - order.get(b.id)!
  right.sort(byOrder)
  left.sort(byOrder)
  return { right, left }
}

// mirrorX espelha uma coordenada ELK do ramo esquerdo sobre a âncora da raiz
// (F-0187): involução pura — mirrorX(mirrorX(x)) === x.
export function mirrorX(x: number, rootX: number, nodeW: number): number {
  return 2 * rootX - x - nodeW
}

export function buildMindmapTree(state: StateResponse, opts: MindmapOptions): MindmapResult {
  const byId = new Map(state.features.map((f) => [f.id, f]))
  const passes = (f: Feature): boolean => {
    if (opts.deliveredOnly) return DELIVERED.has(f.state)
    if (opts.hideTerminal) return !TERMINAL.has(f.state)
    return true
  }

  // parent efetivo: só quando aponta para uma feature existente (id órfão cai
  // no módulo do filho). Ciclos são impossíveis no domínio (ValidateParentLink).
  const parentOf = (f: Feature): string =>
    f.parent && byId.has(f.parent) ? f.parent : ''
  const childrenOf = new Map<string, Feature[]>()
  for (const f of state.features) {
    const p = parentOf(f)
    if (p) {
      const list = childrenOf.get(p) ?? []
      list.push(f)
      childrenOf.set(p, list)
    }
  }

  // Inclusão: passa no filtro, ou tem descendente que passa (estrutura).
  const included = new Map<string, boolean>()
  const include = (f: Feature): boolean => {
    const memo = included.get(f.id)
    if (memo !== undefined) return memo
    const keep = passes(f) || (childrenOf.get(f.id) ?? []).some(include)
    included.set(f.id, keep)
    return keep
  }

  let featureCount = 0
  const buildFeature = (f: Feature): MindNode => {
    const ok = passes(f)
    if (ok) featureCount++
    const kids = (childrenOf.get(f.id) ?? [])
      .filter(include)
      .sort((a, b) => a.id.localeCompare(b.id))
      .map(buildFeature)
    return prune({
      id: f.id,
      label: f.title,
      kind: 'feature',
      state: f.state,
      passesFilter: ok,
      hiddenDescendants: 0,
      children: kids,
    })
  }

  // Colapso: poda os filhos e conta as features descendentes ocultas.
  const countFeatures = (n: MindNode): number =>
    (n.kind === 'feature' ? 1 : 0) +
    n.hiddenDescendants +
    n.children.reduce((sum, c) => sum + countFeatures(c), 0)
  const prune = (n: MindNode): MindNode => {
    if (!opts.collapsed.has(n.id) || n.children.length === 0) return n
    return {
      ...n,
      hiddenDescendants: n.children.reduce((sum, c) => sum + countFeatures(c), 0),
      children: [],
    }
  }

  // Raízes (sem parent efetivo) agrupadas por módulo; módulos em ordem
  // alfabética, com o balde sem-módulo ("") por último.
  const roots = state.features.filter((f) => !parentOf(f) && include(f))
  const byModule = new Map<string, Feature[]>()
  for (const f of roots) {
    const list = byModule.get(f.module) ?? []
    list.push(f)
    byModule.set(f.module, list)
  }
  const moduleNames = [...byModule.keys()].sort((a, b) => {
    if (a === '') return 1
    if (b === '') return -1
    return a.localeCompare(b)
  })
  const moduleNodes = moduleNames.map((name) =>
    prune({
      id: `module:${name}`,
      label: name,
      kind: 'module',
      hiddenDescendants: 0,
      children: byModule
        .get(name)!
        .sort((a, b) => a.id.localeCompare(b.id))
        .map(buildFeature),
    }),
  )

  const root = prune({
    id: 'root',
    label: state.project.name,
    kind: 'root',
    hiddenDescendants: 0,
    children: moduleNodes,
  })
  return { root, featureCount }
}
