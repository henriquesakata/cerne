import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import App from './App'
import { search } from './api'
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
