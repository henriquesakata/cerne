import { describe, expect, it } from 'vitest'
import {
  branchIds,
  buildCapabilityTree,
  buildMindmapTree,
  mirrorX,
  splitSides,
  type MindNode,
} from './mindmap-tree'
import type { Capability, Feature, FeatureState, StateResponse } from './api'

// Fixtures mínimas: só os campos que a árvore consome importam.
function mkFeature(
  id: string,
  over: Partial<Pick<Feature, 'module' | 'parent' | 'state' | 'title'>> = {},
): Feature {
  return {
    id,
    title: over.title ?? `title ${id}`,
    module: over.module ?? '',
    parent: over.parent,
    state: (over.state ?? 'implemented') as FeatureState,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function mkState(features: Feature[], modules: string[] = []): StateResponse {
  return {
    project: { name: 'proj', modules },
    features,
    decisions: [],
    references: [],
    runbooks: [],
    notes: [],
  }
}

const DEFAULTS = { hideTerminal: true, deliveredOnly: false, collapsed: new Set<string>() }

function byId(node: MindNode, id: string): MindNode | null {
  if (node.id === id) return node
  for (const c of node.children) {
    const hit = byId(c, id)
    if (hit) return hit
  }
  return null
}

describe('buildMindmapTree', () => {
  // Critério 1: raiz = projeto, 1º nível = módulos + balde sem-módulo (por último).
  it('groups features under module nodes, no-module bucket last', () => {
    const { root, featureCount } = buildMindmapTree(
      mkState(
        [
          mkFeature('F-0001', { module: 'beta' }),
          mkFeature('F-0002', { module: 'alpha' }),
          mkFeature('F-0003', { module: '' }),
        ],
        ['alpha', 'beta'],
      ),
      DEFAULTS,
    )
    expect(root.kind).toBe('root')
    expect(root.label).toBe('proj')
    expect(root.children.map((c) => c.id)).toEqual(['module:alpha', 'module:beta', 'module:'])
    expect(byId(root, 'module:alpha')!.children.map((c) => c.id)).toEqual(['F-0002'])
    expect(byId(root, 'module:')!.children.map((c) => c.id)).toEqual(['F-0003'])
    expect(featureCount).toBe(3)
  })

  // Critério 1: módulo sem nenhuma feature não vira nó.
  it('omits modules with no features', () => {
    const { root } = buildMindmapTree(
      mkState([mkFeature('F-0001', { module: 'alpha' })], ['alpha', 'empty']),
      DEFAULTS,
    )
    expect(root.children.map((c) => c.id)).toEqual(['module:alpha'])
  })

  // Critério 2: parent vence module — o filho aninha sob o épico (ramo do épico),
  // nunca sob o próprio módulo; profundidade arbitrária.
  it('nests by parent, parent wins over child module, arbitrary depth', () => {
    const { root } = buildMindmapTree(
      mkState([
        mkFeature('F-0001', { module: 'alpha' }), // épico
        mkFeature('F-0002', { module: 'beta', parent: 'F-0001' }),
        mkFeature('F-0003', { parent: 'F-0002' }), // neto
      ]),
      DEFAULTS,
    )
    expect(byId(root, 'module:alpha')).not.toBeNull()
    expect(byId(root, 'module:beta')).toBeNull() // beta ficou vazio → omitido
    const epic = byId(root, 'F-0001')!
    expect(epic.children.map((c) => c.id)).toEqual(['F-0002'])
    expect(byId(epic, 'F-0003')).not.toBeNull()
    // nó único: F-0002 só aparece uma vez na árvore inteira
    const seen: string[] = []
    const walk = (n: MindNode) => {
      seen.push(n.id)
      n.children.forEach(walk)
    }
    walk(root)
    expect(seen.filter((id) => id === 'F-0002')).toHaveLength(1)
  })

  // Robustez: parent apontando para id inexistente cai no módulo do filho.
  it('falls back to own module when parent is unknown', () => {
    const { root } = buildMindmapTree(
      mkState([mkFeature('F-0001', { module: 'alpha', parent: 'F-9999' })]),
      DEFAULTS,
    )
    expect(byId(root, 'module:alpha')!.children.map((c) => c.id)).toEqual(['F-0001'])
  })

  // Critério 5: filtros de estado + contagem.
  it('hides terminal states by default and counts only passing features', () => {
    const { root, featureCount } = buildMindmapTree(
      mkState([
        mkFeature('F-0001', { state: 'implemented' }),
        mkFeature('F-0002', { state: 'superseded' }),
        mkFeature('F-0003', { state: 'rejected' }),
      ]),
      DEFAULTS,
    )
    expect(byId(root, 'F-0002')).toBeNull()
    expect(byId(root, 'F-0003')).toBeNull()
    expect(featureCount).toBe(1)
  })

  it('deliveredOnly keeps only implemented and done', () => {
    const { root, featureCount } = buildMindmapTree(
      mkState([
        mkFeature('F-0001', { state: 'implemented' }),
        mkFeature('F-0002', { state: 'done' }),
        mkFeature('F-0003', { state: 'in_development' }),
        mkFeature('F-0004', { state: 'candidate' }),
      ]),
      { ...DEFAULTS, deliveredOnly: true },
    )
    expect(byId(root, 'F-0001')).not.toBeNull()
    expect(byId(root, 'F-0002')).not.toBeNull()
    expect(byId(root, 'F-0003')).toBeNull()
    expect(byId(root, 'F-0004')).toBeNull()
    expect(featureCount).toBe(2)
  })

  // Critério 5 (estrutura): ancestral filtrado de um visível permanece como
  // estrutura (passesFilter=false), e não entra na contagem.
  it('keeps a filtered epic as structure when a descendant passes', () => {
    const { root, featureCount } = buildMindmapTree(
      mkState([
        mkFeature('F-0001', { module: 'alpha', state: 'in_development' }), // épico fora do filtro
        mkFeature('F-0002', { parent: 'F-0001', state: 'implemented' }),
      ]),
      { ...DEFAULTS, deliveredOnly: true },
    )
    const epic = byId(root, 'F-0001')!
    expect(epic).not.toBeNull()
    expect(epic.passesFilter).toBe(false)
    expect(byId(root, 'F-0002')!.passesFilter).toBe(true)
    expect(featureCount).toBe(1)
  })

  // Critério 4 (parte pura): colapso poda a subárvore e conta descendentes ocultos.
  it('prunes collapsed subtrees and reports hidden descendants', () => {
    const { root } = buildMindmapTree(
      mkState([
        mkFeature('F-0001', { module: 'alpha' }),
        mkFeature('F-0002', { parent: 'F-0001' }),
        mkFeature('F-0003', { parent: 'F-0002' }),
      ]),
      { ...DEFAULTS, collapsed: new Set(['F-0001']) },
    )
    const epic = byId(root, 'F-0001')!
    expect(epic.children).toHaveLength(0)
    expect(epic.hiddenDescendants).toBe(2)
    expect(byId(root, 'module:alpha')!.hiddenDescendants).toBe(0)
  })

  it('collapses module nodes too', () => {
    const { root } = buildMindmapTree(
      mkState([mkFeature('F-0001', { module: 'alpha' }), mkFeature('F-0002', { module: 'alpha' })]),
      { ...DEFAULTS, collapsed: new Set(['module:alpha']) },
    )
    const mod = byId(root, 'module:alpha')!
    expect(mod.children).toHaveLength(0)
    expect(mod.hiddenDescendants).toBe(2)
  })

  // F-0188 crit. 8/9: árvore por capacidade — projeção pura da taxonomia com
  // as features penduradas por nó.
  describe('buildCapabilityTree', () => {
    const mkCap = (id: string, over: Partial<Capability> = {}): Capability => ({
      id,
      title: over.title ?? `cap ${id}`,
      parent: over.parent,
      state: over.state ?? 'active',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    })
    const capState = (caps: Capability[], features: Feature[]): StateResponse => ({
      ...mkState(features),
      capabilities: caps,
    })

    it('builds the taxonomy tree with linked features per node', () => {
      const { root, featureCount } = buildCapabilityTree(
        capState(
          [
            mkCap('C-0001', { title: 'Studio' }),
            mkCap('C-0002', { title: 'Issues', parent: 'C-0001' }),
          ],
          [
            mkFeature('F-0001', { module: 'x' }),
            { ...mkFeature('F-0002'), capabilities: ['C-0002'] },
            { ...mkFeature('F-0003'), capabilities: ['C-0002', 'C-0001'] },
          ],
        ),
        { collapsed: new Set() },
      )
      expect(root.kind).toBe('root')
      const studio = byId(root, 'C-0001')!
      expect(studio.kind).toBe('capability')
      expect(studio.children.map((c) => c.id)).toEqual(['C-0002'])
      expect(studio.features).toEqual(['F-0003'])
      expect(byId(root, 'C-0002')!.features).toEqual(['F-0002', 'F-0003'])
      // featureCount = features distintas vinculadas a ≥1 capacidade
      expect(featureCount).toBe(2)
    })

    it('hides retired capabilities and orphans their children to the root', () => {
      const { root } = buildCapabilityTree(
        capState(
          [
            mkCap('C-0001', { state: 'superseded' }),
            mkCap('C-0002', { parent: 'C-0001' }),
          ],
          [],
        ),
        { collapsed: new Set() },
      )
      expect(byId(root, 'C-0001')).toBeNull()
      expect(root.children.map((c) => c.id)).toEqual(['C-0002'])
    })

    it('prunes collapsed capability subtrees and is deterministic', () => {
      const state = capState(
        [mkCap('C-0001'), mkCap('C-0002', { parent: 'C-0001' })],
        [{ ...mkFeature('F-0001'), capabilities: ['C-0002'] }],
      )
      const a = buildCapabilityTree(state, { collapsed: new Set(['C-0001']) })
      const node = byId(a.root, 'C-0001')!
      expect(node.children).toHaveLength(0)
      expect(node.hiddenDescendants).toBe(1)
      expect(buildCapabilityTree(state, { collapsed: new Set(['C-0001']) })).toEqual(a)
    })
  })

  // F-0190 crit. 1: conjunto do "tudo recolhido" — todo nó com filhos, raiz fora.
  it('branchIds collects every node with children, excluding the root', () => {
    const { root } = buildMindmapTree(
      mkState([
        mkFeature('F-0001', { module: 'alpha' }), // épico
        mkFeature('F-0002', { parent: 'F-0001' }),
        mkFeature('F-0003', { parent: 'F-0002' }), // neto → F-0002 também é ramo
        mkFeature('F-0004', { module: 'beta' }), // folha
      ]),
      DEFAULTS,
    )
    const ids = branchIds(root)
    expect(ids).toEqual(['F-0001', 'F-0002', 'module:alpha', 'module:beta'])
    expect(ids).not.toContain('root')
    expect(branchIds(root)).toEqual(ids) // determinístico
  })

  // F-0187 crit. 1/3: divisão bilateral greedy por peso da subárvore visível.
  describe('splitSides', () => {
    const leaf = (id: string): MindNode => ({
      id,
      label: id,
      kind: 'feature',
      state: 'implemented',
      passesFilter: true,
      hiddenDescendants: 0,
      children: [],
    })
    const branch = (id: string, size: number): MindNode => ({
      id: `module:${id}`,
      label: id,
      kind: 'module',
      hiddenDescendants: 0,
      children: Array.from({ length: size - 1 }, (_, i) => leaf(`${id}-${i}`)),
    })
    const mkRoot = (children: MindNode[]): MindNode => ({
      id: 'root',
      label: 'proj',
      kind: 'root',
      hiddenDescendants: 0,
      children,
    })

    it('balances greedily by visible subtree weight', () => {
      // pesos (nós renderizados): a=5, b=3, c=2, d=2
      const root = mkRoot([branch('a', 5), branch('b', 3), branch('c', 2), branch('d', 2)])
      const { right, left } = splitSides(root)
      expect(right.map((n) => n.label)).toEqual(['a', 'd'])
      expect(left.map((n) => n.label)).toEqual(['b', 'c'])
    })

    it('preserves original display order within each side', () => {
      const root = mkRoot([branch('a', 2), branch('b', 5), branch('c', 2)])
      const { right, left } = splitSides(root)
      // b (5) → direita; a (2) → esquerda; c (2): esq(2) < dir(5) → esquerda.
      expect(right.map((n) => n.label)).toEqual(['b'])
      expect(left.map((n) => n.label)).toEqual(['a', 'c'])
    })

    it('is deterministic with id tiebreak and single branch goes right', () => {
      const root = mkRoot([branch('x', 3)])
      const a = splitSides(root)
      expect(a.right.map((n) => n.label)).toEqual(['x'])
      expect(a.left).toEqual([])
      const twice = mkRoot([branch('b', 2), branch('a', 2)])
      expect(splitSides(twice)).toEqual(splitSides(twice))
      // empate de peso → menor id primeiro na alocação (a → direita)
      expect(splitSides(twice).right.map((n) => n.label)).toEqual(['a'])
    })
  })

  // F-0187 crit. 3: espelhamento do lado esquerdo é aritmética pura.
  it('mirrors x relative to the root anchor', () => {
    expect(mirrorX(300, 100, 196)).toBe(2 * 100 - 300 - 196)
    expect(mirrorX(100, 100, 196)).toBe(100 - 196) // a própria raiz espelha sobre si
    expect(mirrorX(mirrorX(300, 100, 196), 100, 196)).toBe(300) // involução
  })

  // Critério 6: determinismo — mesma entrada (em qualquer ordem), mesma árvore.
  it('is deterministic and sorts children by id regardless of input order', () => {
    const shuffled = mkState([
      mkFeature('F-0003', { module: 'alpha' }),
      mkFeature('F-0001', { module: 'alpha' }),
      mkFeature('F-0002', { module: 'alpha' }),
    ])
    const a = buildMindmapTree(shuffled, DEFAULTS)
    const b = buildMindmapTree(shuffled, DEFAULTS)
    expect(a).toEqual(b)
    expect(byId(a.root, 'module:alpha')!.children.map((c) => c.id)).toEqual([
      'F-0001',
      'F-0002',
      'F-0003',
    ])
  })
})
