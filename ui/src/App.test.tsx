import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import App from './App'
import { fetchHandoff, fetchHandoffs, fetchIdeas, fetchPendings, fetchState, promoteIdea, search } from './api'
import type { ProjectInfo, StateResponse } from './api'

const state: StateResponse = {
  project: { name: 'Projeto Demo', modules: [] },
  features: [],
  decisions: [],
  references: [],
  runbooks: [],
  notes: [],
}

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => state),
  fetchProjects: vi.fn(async () => []),
  search: vi.fn(async () => []),
  fetchSettings: vi.fn(async () => ({ settings: [], files: {} })),
  fetchPendings: vi.fn(async () => []),
  fetchIdeas: vi.fn(async () => []),
  fetchHandoffs: vi.fn(async () => []),
  fetchHandoff: vi.fn(async () => { throw new Error('handoff not found') }),
  promoteIdea: vi.fn(),
}))

// Os providers viram passthrough: o alvo aqui é o roteador e a montagem
// global da palette, não o live stream nem as notificações.
vi.mock('./live', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./live')>()),
  LiveProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  useLiveVersion: () => 0,
  useProjects: () => ({ projects: mockProjects, activePid: 'p1' }),
}))

let mockProjects: ProjectInfo[] = []

// O Grafo real puxa React Flow; aqui basta saber se a rota o monta.
vi.mock('./Graph', () => ({ default: () => <p>grafo montado</p> }))

vi.mock('./notifications', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./notifications')>()),
  NotificationsProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}))

// O Nav mede a barra com ResizeObserver, que o jsdom não implementa.
class NoopResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= NoopResizeObserver as unknown as typeof ResizeObserver

beforeEach(() => {
  vi.mocked(search).mockClear()
  mockProjects = []
})
afterEach(cleanup)

describe('App — palette e rota de busca (F-0196)', () => {
  // C12: a palette é global — vale em qualquer rota, não só no kanban.
  it('abre a palette fora do kanban', async () => {
    window.location.hash = '#/wiki'
    render(<App />)

    fireEvent.keyDown(document, { key: 'k', metaKey: true })
    await waitFor(() => expect(screen.getByRole('combobox')).toBeInTheDocument())
  })

  // C9: #/search/<termo> cai na busca já executada.
  it('roteia #/search/<termo> para a busca com o termo aplicado', async () => {
    window.location.hash = '#/search/badge%20novo'
    render(<App />)

    await waitFor(() => expect(search).toHaveBeenCalledWith('badge novo'))
  })

  // C9 (regressão): a rota sem termo continua inerte até o submit.
  it('mantém #/search sem termo inerte', async () => {
    window.location.hash = '#/search'
    render(<App />)

    await waitFor(() => expect(screen.getByRole('textbox')).toBeInTheDocument())
    expect(search).not.toHaveBeenCalled()
  })
})

describe('App — rota de Settings (F-0315)', () => {
  it('#/settings monta a tela e a engrenagem da nav fica marcada como página atual', async () => {
    window.location.hash = '#/settings'
    render(<App />)

    expect(await screen.findByRole('heading', { level: 1, name: 'Configurações' })).toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'Configurações' })
    expect(link).toHaveAttribute('href', '#/settings')
    expect(link).toHaveAttribute('aria-current', 'page')
    // Settings não é seção de LINKS: nenhum link de seção fica ativo por fallback.
    expect(screen.getByRole('link', { name: 'Dashboard' })).not.toHaveClass('active')
  })
})

describe('App — rota do Grafo governada por graph.enabled (F-0315)', () => {
  it('com graph desligado, #/graph cai no dashboard e não monta o Grafo', async () => {
    mockProjects = [{ pid: 'p1', name: 'P1', graph: false }]
    window.location.hash = '#/graph'
    render(<App />)
    await waitFor(() => expect(screen.getByRole('main')).toBeInTheDocument())
    expect(screen.queryByText('grafo montado')).toBeNull()
  })

  it('com graph ligado, #/graph monta o Grafo', async () => {
    mockProjects = [{ pid: 'p1', name: 'P1', graph: true }]
    window.location.hash = '#/graph'
    render(<App />)
    expect(await screen.findByText('grafo montado')).toBeInTheDocument()
  })
})

describe('App — deep link para uma aba desligada vira #/ (F-0315, crítica C15)', () => {
  it.each([
    ['#/graph', { graph: false }],
    ['#/mindmap', { mindmap: false }],
  ])('%s com a chave off troca o hash por #/ e a nav marca o Dashboard', async (hash, gate) => {
    mockProjects = [{ pid: 'p1', name: 'P1', ...gate }]
    window.location.hash = hash
    render(<App />)
    await waitFor(() => expect(window.location.hash).toBe('#/'))
    await waitFor(() => expect(screen.getByRole('link', { name: 'Dashboard' })).toHaveClass('active'))
  })
})

// F-0334 crit. 1 e 5: #/pendings aceita filtro no endereço (rota por prefixo)
// e #/ideas monta o painel de ideias real da F-0312.
describe('App — pendências e ideias (F-0334)', () => {
  it('#/pendings?from=… monta a tela de pendências já filtrada', async () => {
    window.location.hash = '#/pendings?from=F-0331'
    render(<App />)
    expect(await screen.findByRole('heading', { level: 1, name: 'Pendências' })).toBeInTheDocument()
    await waitFor(() => expect(fetchPendings).toHaveBeenCalledWith({ state: 'all', from: 'F-0331', component: '' }))
  })

  it.each(['#/pendingsfoo', '#/ideas?x=1'])('%s não monta pendências nem ideias (a mesma regra da nav)', async (hash) => {
    window.location.hash = hash
    render(<App />)
    expect(await screen.findByRole('heading', { level: 1, name: 'Dashboard' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Pendências' })).toBeNull()
    expect(screen.queryByPlaceholderText('Anote uma ideia e tecle Enter…')).toBeNull()
  })

  it('#/ideas monta o painel de ideias', async () => {
    window.location.hash = '#/ideas'
    render(<App />)
    expect(await screen.findByRole('heading', { level: 2, name: 'Ideias' })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Anote uma ideia e tecle Enter…')).toBeInTheDocument()
    // Crítica C14: como toda tela, um h1 — oculto, porque o painel já titula.
    const h1 = screen.getByRole('heading', { level: 1, name: 'Ideias' })
    expect(h1).toHaveClass('sr-only')
  })
})

// Crítica C16: sem os módulos o Promover sairia sem --module num projeto que o
// exige; a tela de ideias espera por eles e, se a busca falha, mostra o erro.
describe('App — #/ideas espera os módulos (F-0334, crítica C16)', () => {
  afterEach(() => vi.mocked(fetchState).mockImplementation(async () => state))

  it('com a busca do estado falhando, mostra o erro e não monta o painel', async () => {
    vi.mocked(fetchState).mockRejectedValue(new Error('boom'))
    window.location.hash = '#/ideas'
    render(<App />)
    expect(await screen.findByText('Erro ao carregar o estado: boom')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Tentar de novo' })).toBeInTheDocument()
    expect(screen.queryByPlaceholderText('Anote uma ideia e tecle Enter…')).toBeNull()
  })

  it('enquanto os módulos não chegam, o painel não aparece', async () => {
    vi.mocked(fetchState).mockImplementation(() => new Promise(() => {}))
    window.location.hash = '#/ideas'
    render(<App />)
    expect(await screen.findByText('Carregando…')).toBeInTheDocument()
    expect(screen.queryByPlaceholderText('Anote uma ideia e tecle Enter…')).toBeNull()
  })
})

// Crítica C5: no Backlog a candidata nova aparece ao lado do painel; nesta
// porta nada mudava. O wrapper anuncia a promoção e leva ao Backlog.
describe('App — #/ideas anuncia a promoção (F-0334, crítica C5)', () => {
  afterEach(() => vi.mocked(fetchIdeas).mockImplementation(async () => []))

  it('promover mostra o aviso com o link para o Backlog', async () => {
    const idea = { id: 'I-0001', text: 'Uma ideia', state: 'active' as const, created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z' }
    vi.mocked(fetchIdeas).mockImplementation(async () => [idea])
    vi.mocked(promoteIdea).mockResolvedValue({ idea: { ...idea, state: 'promoted', promoted_to: 'F-0009' }, feature: {} as never })
    window.location.hash = '#/ideas'
    render(<App />)
    const status = await screen.findByRole('status')
    expect(status).toBeEmptyDOMElement()
    fireEvent.click(await screen.findByRole('button', { name: 'Promover I-0001' }))
    await waitFor(() => expect(status).toHaveTextContent('Ideia promovida — a candidata está no Backlog.'))
    expect(within(status).getByRole('link', { name: 'Abrir o Backlog' })).toHaveAttribute('href', '#/backlog')
  })
})

// F-0386 crit. 2–4: a área de passagens, a lista com o filtro na URL e o
// detalhe por id; o resto do prefixo cai no Dashboard, como na nav.
describe('App — passagens (F-0386)', () => {
  it.each(['#/handoffs', '#/handoffs?f=closed'])('%s monta a área de passagens', async (hash) => {
    window.location.hash = hash
    render(<App />)
    expect(await screen.findByRole('heading', { level: 1, name: 'Passagens' })).toBeInTheDocument()
    await waitFor(() => expect(fetchHandoffs).toHaveBeenCalled())
  })

  it('#/handoffs/HO-0001 monta o detalhe daquela passagem', async () => {
    window.location.hash = '#/handoffs/HO-0001'
    render(<App />)
    expect(await screen.findByText('Erro ao carregar a passagem: handoff not found')).toBeInTheDocument()
    expect(fetchHandoff).toHaveBeenCalledWith('HO-0001')
  })

  it('#/handoffsfoo não monta a área', async () => {
    window.location.hash = '#/handoffsfoo'
    render(<App />)
    expect(await screen.findByRole('heading', { level: 1, name: 'Dashboard' })).toBeInTheDocument()
  })
})
