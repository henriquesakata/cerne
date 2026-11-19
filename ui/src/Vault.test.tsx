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
    { ...ent('B-0001', 'Quebra', 'approved'), module: 'm' },
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
  fetchWikiIndex: vi.fn(async () => ({
    pages: [
      { name: 'Overview', rel: 'overview.md' },
      { name: 'project', rel: 'project.md' },
    ],
  })),
}))
vi.mock('./vault-doc', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./vault-doc')>()),
  loadVaultDoc: (id: string, doc?: string) => loadVaultDoc(id, doc),
}))
vi.mock('./live', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./live')>()),
  useLiveVersion: () => 0,
}))
vi.mock('./Backlinks', () => ({
  default: ({ id, href }: { id: string; href?: (k: string, i: string) => string }) => (
    <p>
      backlinks de {id} <a href={href?.('note', 'N-0001')}>origem</a>
    </p>
  ),
}))

const mk = (id: string, over: Partial<VaultDoc> = {}): VaultDoc => ({
  id,
  kind: 'decision',
  state: 'active',
  historical: false,
  real: true,
  title: 'T',
  content: 'corpo',
  hash: 'h-' + id,
  ...over,
})

beforeEach(() => {
  window.location.hash = ''
  loadVaultDoc.mockClear()
  Element.prototype.scrollIntoView = vi.fn()
  docs = {
    'D-0001': mk('D-0001', { title: 'Decisão viva', content: 'cita [[N-0001]] e [[N-0099]], [[HO-0001]] e [[W-0001]]' }),
    'D-0002': mk('D-0002', { title: 'Decisão antiga', state: 'superseded', historical: true }),
    'F-0003': mk('F-0003', { kind: 'feature', doc: 'spec', state: 'approved', title: 'Vazia', real: false, content: '# scaffold' }),
    'F-0001': mk('F-0001', { kind: 'feature', doc: 'plan', state: 'in_development', title: 'Primeira', content: '# Primeira\n\no plano' }),
    'N-0001': mk('N-0001', { kind: 'note', title: 'Nota', content: '## A\n\ntexto\n\n### B\n\nmais\n\n```\n## no\n```' }),
    'wiki:overview.md': mk('wiki:overview.md', { kind: 'wiki', state: '', title: 'overview.md' }),
  }
})
afterEach(() => {
  cleanup()
  // @ts-expect-error restore: jsdom has no matchMedia, a test may have stubbed it
  delete window.matchMedia
})

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

  const group = (re: RegExp) => fireEvent.click(within(tree()).getByText(re))

  it('B6: a árvore abre recolhida, só Em andamento aberto, com defeito/tarefa rotulados', async () => {
    await open('#/vault')
    expect(within(tree()).getByText(/^Em andamento \(/)).toBeInTheDocument()
    expect(within(tree()).getByRole('link', { name: /F-0001/ })).toBeInTheDocument()
    expect(within(tree()).getByText('defeito')).toBeInTheDocument()
    // os seis grupos ficam fechados: nada de D-0001 até abrir
    expect(within(tree()).queryByRole('link', { name: /D-0001/ })).toBeNull()
    expect(within(tree()).getByText(/^Decisões \(1\)/)).toBeInTheDocument()
    group(/^Decisões \(/)
    expect(within(tree()).getByRole('link', { name: /D-0001/ })).toBeInTheDocument()
  })

  it('hash: clicar num nó escreve o hash da seleção', async () => {
    await open('#/vault')
    group(/^Decisões \(/)
    fireEvent.click(within(tree()).getByRole('link', { name: /D-0001/ }))
    await waitFor(() => expect(window.location.hash).toBe('#/vault/D-0001'))
    // a feature expande para spec/plan/result
    fireEvent.click(within(tree()).getByRole('button', { name: 'Expandir F-0001' }))
    fireEvent.click(within(tree()).getByRole('link', { name: 'Especificação' }))
    await waitFor(() => expect(window.location.hash).toBe('#/vault/F-0001/spec'))
  })

  it('hash: abrir direto renderiza o doc e expande a árvore até ele', async () => {
    await open('#/vault/F-0001/plan')
    expect(await screen.findByText('o plano')).toBeInTheDocument()
    expect(loadVaultDoc).toHaveBeenCalledWith('F-0001', 'plan')
    const link = within(tree()).getByRole('link', { name: 'Plano' })
    expect(link).toHaveAttribute('aria-current', 'page')
    // B3: o nó selecionado é trazido à vista, sem rolagem suave
    await waitFor(() => expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith({ block: 'nearest' }))
  })

  it('stage: feature sem <doc> carrega pelo estágio (doc indefinido)', async () => {
    await open('#/vault/F-0001')
    expect(await screen.findByText('o plano')).toBeInTheDocument()
    expect(loadVaultDoc).toHaveBeenCalledWith('F-0001', undefined)
    expect(within(tree()).getByRole('link', { name: 'Plano' })).toHaveAttribute('aria-current', 'page')
  })

  it('doc fora da família feature é ignorado: #/vault/D-0001/spec marca a linha da decisão', async () => {
    await open('#/vault/D-0001/spec')
    await screen.findByRole('heading', { name: /D-0001/ })
    expect(loadVaultDoc).toHaveBeenCalledWith('D-0001', undefined)
    expect(within(tree()).getByRole('link', { name: /D-0001/ })).toHaveAttribute('aria-current', 'page')
  })

  it('A4: #/vault/d-0001 abre D-0001', async () => {
    await open('#/vault/d-0001')
    await screen.findByRole('heading', { name: /D-0001/ })
    expect(loadVaultDoc).toHaveBeenCalledWith('D-0001', undefined)
  })

  it('histórico: oculto por padrão, o controle mostra', async () => {
    await open('#/vault')
    group(/^Decisões \(/)
    expect(within(tree()).queryByRole('link', { name: /D-0002/ })).toBeNull()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Mostrar histórico' }))
    expect(within(tree()).getByRole('link', { name: /D-0002/ })).toBeInTheDocument()
  })

  it('B4: com o histórico ligado, só o item histórico leva selo, em português', async () => {
    await open('#/vault')
    fireEvent.click(screen.getByRole('checkbox', { name: 'Mostrar histórico' }))
    group(/^Decisões \(/)
    const row = (id: string) => within(tree()).getByRole('link', { name: new RegExp(id) }).closest('li')!
    expect(row('D-0002')).toHaveTextContent('Substituída')
    expect(row('D-0002')).not.toHaveTextContent('superseded')
    expect(row('D-0001').querySelector('.chip')).toBeNull()
    group(/^Páginas da wiki \(/)
    expect(within(tree()).getByRole('link', { name: 'Projeto' })).toBeInTheDocument()
  })

  it('filtro: id ou título sem acento', async () => {
    await open('#/vault')
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filtrar a árvore' }), { target: { value: 'decisao' } })
    expect(within(tree()).getByRole('link', { name: /D-0001/ })).toBeInTheDocument()
    expect(within(tree()).queryByRole('link', { name: /N-0001/ })).toBeNull()
  })

  it('filtro sem resultado oferece a busca de texto', async () => {
    await open('#/vault')
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filtrar a árvore' }), { target: { value: 'zzz qq' } })
    expect(within(tree()).getByRole('link', { name: /Buscar no texto/ })).toHaveAttribute('href', '#/search/zzz%20qq')
  })

  it('wiki-link no corpo abre a entidade no cofre; desconhecido sai marcado; HO e W- como fora do cofre (A3)', async () => {
    const { container } = await open('#/vault/D-0001')
    const body = () => container.querySelector('.vault-doc-body')!
    await waitFor(() => expect(body().querySelector('a.wikilink')).not.toBeNull())
    const hrefs = [...body().querySelectorAll('a.wikilink')].map((a) => a.getAttribute('href'))
    expect(hrefs).toEqual(['#/vault/N-0001', '#/handoffs/HO-0001'])
    expect([...body().querySelectorAll('.wikilink-broken')].map((e) => e.textContent)).toEqual(['N-0099'])
    expect(body().textContent).toContain('[[W-0001]]')
  })

  it('historical: selo, sem ação de edição; visitante fixo no topo e o controle intacto (B3)', async () => {
    const { container, rerender } = await open('#/vault/D-0002')
    expect(await screen.findByRole('heading', { name: /D-0002/ })).toBeInTheDocument()
    expect(document.querySelector('.vault-doc-header .chip')).toHaveTextContent('Substituída')
    expect(container.querySelector('.vault-doc-actions')).toBeEmptyDOMElement()
    expect(screen.getByRole('checkbox', { name: 'Mostrar histórico' })).not.toBeChecked()
    expect(within(tree()).getByText(/^Visitando \(1\)/)).toBeInTheDocument()
    expect(within(tree()).getByRole('link', { name: /D-0002/ })).toHaveAttribute('aria-current', 'page')
    // ao ir para um doc vivo o visitante some, e o controle segue desligado
    rerender(<Vault hash="#/vault/D-0001" />)
    await screen.findByRole('heading', { name: /D-0001/ })
    expect(within(tree()).queryByText(/^Visitando/)).toBeNull()
    expect(screen.getByRole('checkbox', { name: 'Mostrar histórico' })).not.toBeChecked()
  })

  it('B3: cabeçalho com estado traduzido, abas Especificação/Plano/Resultado e um só título', async () => {
    const { container } = await open('#/vault/F-0001')
    await screen.findByText('o plano')
    expect(document.querySelector('.vault-doc-header .chip')).toHaveTextContent('Em desenvolvimento')
    const tabs = within(screen.getByRole('navigation', { name: 'Documentos da feature' }))
    expect(tabs.getByRole('link', { name: 'Plano' })).toHaveAttribute('aria-current', 'page')
    expect(tabs.getByRole('link', { name: 'Resultado' })).toHaveAttribute('href', '#/vault/F-0001/result')
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(container.querySelector('.vault-doc-body')).toHaveClass('doc')
  })

  it('B2: doc só com scaffold mostra "Ainda não escrito" em vez do conteúdo', async () => {
    const { container } = await open('#/vault/F-0003')
    expect(await screen.findByText('Ainda não escrito')).toBeInTheDocument()
    expect(container.querySelector('.vault-doc-body')).toBeNull()
  })

  it('outline: lista ##/### fora de bloco de código e rola até a seção', async () => {
    await open('#/vault/N-0001')
    const outline = await screen.findByRole('navigation', { name: 'Neste documento' })
    const items = within(outline).getAllByRole('button')
    expect(items.map((b) => b.textContent)).toEqual(['A', 'B'])
    fireEvent.click(items[1])
    expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith({ block: 'start' })
  })

  it('backlinks: painel da entidade aberta, links dentro do cofre (B5), nenhum para wiki', async () => {
    await open('#/vault/D-0001')
    expect(await screen.findByText('backlinks de D-0001')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'origem' })).toHaveAttribute('href', '#/vault/N-0001')
    cleanup()
    await open('#/vault/wiki:overview.md')
    await screen.findByText('corpo')
    expect(screen.queryByText(/backlinks de/)).toBeNull()
  })

  it('B8: em tela estreita, tocar num doc da árvore leva o leitor à vista', async () => {
    window.matchMedia = ((q: string) => ({ matches: q.includes('720px') })) as unknown as typeof window.matchMedia
    await open('#/vault')
    ;(Element.prototype.scrollIntoView as ReturnType<typeof vi.fn>).mockClear()
    fireEvent.click(within(tree()).getByRole('link', { name: /F-0001/ }))
    expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith({ block: 'start' })
  })
})
