import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Vault, { vaultHref } from './Vault'
import type { StateResponse } from './api'
import type { VaultDoc } from './vault-doc'

const T = '2026-01-01T00:00:00Z'
const ent = (id: string, title: string, state: string) => ({ id, title, state, created_at: T, updated_at: T })
const state = {
  project: { name: 'p', modules: [] },
  features: [
    { ...ent('F-0001', 'Primeira', 'in_development'), module: 'm' },
    { ...ent('F-0002', 'Velha', 'done'), module: 'm' },
  ],
  decisions: [ent('D-0001', 'Decisão viva', 'active'), ent('D-0002', 'Decisão antiga', 'superseded')],
  notes: [ent('N-0001', 'Nota', 'active')],
  runbooks: [],
  references: [],
} as unknown as StateResponse

let docs: Record<string, VaultDoc> = {}
const loadVaultDoc = vi.fn(async (id: string, doc?: string) => {
  const d = docs[id]
  if (!d) throw new Error(`no doc ${id}`)
  return doc ? { ...d, doc: doc as VaultDoc['doc'] } : d
})

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => state),
  fetchWikiIndex: vi.fn(async () => ({ pages: [{ name: 'Overview', rel: 'overview.md' }] })),
}))
vi.mock('./vault-doc', () => ({ loadVaultDoc: (id: string, doc?: string) => loadVaultDoc(id, doc) }))
vi.mock('./live', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./live')>()),
  useLiveVersion: () => 0,
}))
vi.mock('./Backlinks', () => ({ default: ({ id }: { id: string }) => <p>backlinks de {id}</p> }))

const mk = (id: string, over: Partial<VaultDoc> = {}): VaultDoc => ({
  id,
  kind: 'decision',
  state: 'active',
  historical: false,
  title: 'T',
  content: 'corpo',
  ...over,
})

beforeEach(() => {
  window.location.hash = ''
  loadVaultDoc.mockClear()
  Element.prototype.scrollIntoView = vi.fn()
  docs = {
    'D-0001': mk('D-0001', { title: 'Decisão viva', content: 'cita [[N-0001]] e [[N-0099]]' }),
    'D-0002': mk('D-0002', { title: 'Decisão antiga', state: 'superseded', historical: true }),
    'F-0001': mk('F-0001', { kind: 'feature', doc: 'plan', state: 'in_development', title: 'Primeira', content: 'o plano' }),
    'N-0001': mk('N-0001', { kind: 'note', title: 'Nota', content: '## A\n\ntexto\n\n### B\n\nmais\n\n```\n## no\n```' }),
    'wiki:overview.md': mk('wiki:overview.md', { kind: 'wiki', state: '', title: 'overview.md' }),
  }
})
afterEach(cleanup)

const tree = () => screen.getByRole('navigation', { name: 'Documentos do projeto' })
const open = async (hash: string) => {
  const r = render(<Vault hash={hash} />)
  await screen.findByRole('navigation', { name: 'Documentos do projeto' })
  return r
}

describe('Vault (F-0463)', () => {
  it('vaultHref monta #/vault/<id>[/<doc>]', () => {
    expect(vaultHref('D-0001')).toBe('#/vault/D-0001')
    expect(vaultHref('F-0001', 'spec')).toBe('#/vault/F-0001/spec')
    expect(vaultHref('wiki:modules/x.md')).toBe('#/vault/wiki:modules/x.md')
  })

  it('hash: clicar num nó escreve o hash da seleção', async () => {
    await open('#/vault')
    fireEvent.click(await within(tree()).findByRole('link', { name: /D-0001/ }))
    await waitFor(() => expect(window.location.hash).toBe('#/vault/D-0001'))
    // a feature expande para spec/plan/result
    fireEvent.click(within(tree()).getByRole('button', { name: /F-0001/ }))
    fireEvent.click(within(tree()).getByRole('link', { name: 'Especificação' }))
    await waitFor(() => expect(window.location.hash).toBe('#/vault/F-0001/spec'))
  })

  it('hash: abrir direto renderiza o doc e expande a árvore até ele', async () => {
    await open('#/vault/F-0001/plan')
    expect(await screen.findByText('o plano')).toBeInTheDocument()
    expect(loadVaultDoc).toHaveBeenCalledWith('F-0001', 'plan')
    const link = within(tree()).getByRole('link', { name: 'Plano' })
    expect(link).toHaveAttribute('aria-current', 'page')
  })

  it('stage: feature sem <doc> carrega pelo estágio (doc indefinido)', async () => {
    await open('#/vault/F-0001')
    expect(await screen.findByText('o plano')).toBeInTheDocument()
    expect(loadVaultDoc).toHaveBeenCalledWith('F-0001', undefined)
    expect(within(tree()).getByRole('link', { name: 'Plano' })).toHaveAttribute('aria-current', 'page')
  })

  it('histórico: oculto por padrão, o controle mostra', async () => {
    await open('#/vault')
    await within(tree()).findByRole('link', { name: /D-0001/ })
    expect(within(tree()).queryByRole('link', { name: /D-0002/ })).toBeNull()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Mostrar histórico' }))
    expect(within(tree()).getByRole('link', { name: /D-0002/ })).toBeInTheDocument()
  })

  it('filtro: id ou título sem acento', async () => {
    await open('#/vault')
    await within(tree()).findByRole('link', { name: /D-0001/ })
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filtrar a árvore' }), { target: { value: 'decisao' } })
    expect(within(tree()).getByRole('link', { name: /D-0001/ })).toBeInTheDocument()
    expect(within(tree()).queryByRole('link', { name: /N-0001/ })).toBeNull()
  })

  it('wiki-link no corpo abre a entidade no cofre; id desconhecido sai marcado', async () => {
    const { container } = await open('#/vault/D-0001')
    await waitFor(() => expect(container.querySelector('.vault-doc-body a.wikilink')).not.toBeNull())
    expect(container.querySelector('.vault-doc-body a.wikilink')).toHaveAttribute('href', '#/vault/N-0001')
    expect(container.querySelector('.vault-doc-body .wikilink-broken')?.textContent).toBe('N-0099')
  })

  it('historical: selo do estado, sem ação de edição, e o histórico aparece na árvore', async () => {
    const { container } = await open('#/vault/D-0002')
    expect(await screen.findByRole('heading', { name: /D-0002/ })).toBeInTheDocument()
    expect(document.querySelector('.vault-doc-header .chip')).toHaveTextContent('superseded')
    expect(container.querySelector('.vault-doc-actions')).toBeEmptyDOMElement()
    expect(within(tree()).getByRole('link', { name: /D-0002/ })).toBeInTheDocument()
  })
})
