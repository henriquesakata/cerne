import { describe, expect, it } from 'vitest'
import type { StateResponse, WikiIndex } from './api'
import { buildVaultTree, filterVaultTree, isHistorical, type VaultNode } from './vault-tree'

const T = '2026-01-01T00:00:00Z'
const feat = (id: string, title: string, state: string, parent?: string) =>
  ({ id, title, module: 'm', state, parent, created_at: T, updated_at: T }) as StateResponse['features'][number]
const ent = (id: string, title: string, state: string) => ({ id, title, state, created_at: T, updated_at: T })

const state = {
  project: { name: 'p', modules: [] },
  features: [
    feat('E-0001', 'Épico um', 'in_development'),
    feat('F-0001', 'Filha', 'approved', 'E-0001'),
    feat('F-0002', 'Solta', 'candidate'),
    feat('F-0003', 'Concluída', 'done', 'E-0001'),
    feat('F-0004', 'Órfã de pai oculto', 'approved', 'F-0003'),
  ],
  decisions: [ent('D-0001', 'Decisão viva', 'active'), ent('D-0002', 'Decisão velha', 'retired')],
  notes: [ent('N-0001', 'Nota', 'active')],
  runbooks: [ent('RB-0001', 'Runbook', 'obsolete')],
  references: [ent('R-0001', 'Fonte', 'current')],
} as unknown as StateResponse
const wiki: WikiIndex = { pages: [{ name: 'Overview', rel: 'overview.md' }] }

const find = (nodes: VaultNode[], key: string): VaultNode | undefined => {
  for (const n of nodes) {
    if (n.key === key) return n
    const r = find(n.children, key)
    if (r) return r
  }
}

describe('vault-tree (F-0463)', () => {
  it('critério 1: grupos na ordem, com a contagem de itens', () => {
    const tree = buildVaultTree(state, wiki, { history: false })
    expect(tree.map((g) => g.title)).toEqual(['epics', 'decisions', 'notes', 'runbooks', 'references', 'wiki'])
    // E-0001, F-0001, F-0002, F-0004 (o pai F-0003 está oculto)
    expect(tree.map((g) => g.count)).toEqual([4, 1, 1, 0, 1, 1])
  })

  it('critério 2: feature com parent aninha sob o épico; cada feature tem spec/plan/result', () => {
    const tree = buildVaultTree(state, wiki, { history: false })
    const epic = find(tree, 'E-0001')!
    expect(epic.children.filter((c) => !c.doc).map((c) => c.id)).toEqual(['F-0001'])
    expect(find(tree, 'F-0001')!.children.map((c) => c.doc)).toEqual(['spec', 'plan', 'result'])
    // pai oculto: o filho sobe para a raiz do grupo
    expect(tree[0].children.map((c) => c.id)).toContain('F-0004')
  })

  it('critério 3: histórico oculto por padrão; ligado, mostra com o estado', () => {
    const off = buildVaultTree(state, wiki, { history: false })
    expect(find(off, 'F-0003')).toBeUndefined()
    expect(find(off, 'D-0002')).toBeUndefined()
    expect(find(off, 'RB-0001')).toBeUndefined()
    const on = buildVaultTree(state, wiki, { history: true })
    expect(find(on, 'F-0003')!.state).toBe('done')
    expect(find(on, 'D-0002')!.state).toBe('retired')
    expect(find(on, 'F-0004')!.id).toBe('F-0004')
    expect(find(on, 'F-0003')!.children.map((c) => c.id)).toContain('F-0004')
  })

  it('isHistorical cobre cada tipo', () => {
    expect(isHistorical('feature', 'rejected')).toBe(true)
    expect(isHistorical('feature', 'implemented')).toBe(false)
    expect(isHistorical('decision', 'superseded')).toBe(true)
    expect(isHistorical('reference', 'current')).toBe(false)
    expect(isHistorical('note', 'active')).toBe(false)
  })

  it('critério 4: filtro sem acento nem caixa, mantendo ancestrais', () => {
    const tree = buildVaultTree(state, wiki, { history: false })
    const out = filterVaultTree(tree, 'DECISAO viva')
    expect(out.map((g) => g.title)).toEqual(['decisions'])
    expect(out[0].count).toBe(1)
    const byId = filterVaultTree(tree, 'f-0001')
    expect(find(byId, 'E-0001')).toBeDefined() // ancestral
    expect(find(byId, 'F-0001/spec')).toBeDefined()
    expect(find(byId, 'F-0002')).toBeUndefined()
    expect(filterVaultTree(tree, 'epico')[0].children.map((c) => c.key)).toEqual(['E-0001'])
  })
})
