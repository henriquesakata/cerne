import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, act, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { LiveProvider, useProjects } from './live'
import { fetchProjects } from './api'
import type { ProjectInfo } from './api'

// F-0209: trocar de projeto pelo seletor sempre aterrissa no dashboard —
// nenhuma rota de detalhe sobrevive à troca.

let mockProjects: ProjectInfo[] = [
  { pid: 'p1', name: 'P1' },
  { pid: 'p2', name: 'P2' },
]

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchProjects: vi.fn(),
  setApiProject: vi.fn(),
  subscribeLive: vi.fn(() => () => {}),
}))

function Probe() {
  const { activePid, setActivePid } = useProjects()
  return (
    <button onClick={() => setActivePid(activePid === 'p1' ? 'p2' : 'p1')}>
      {activePid}
    </button>
  )
}

beforeEach(() => {
  cleanup()
  localStorage.clear()
  sessionStorage.clear()
  window.location.hash = ''
  vi.mocked(fetchProjects).mockReset()
  vi.mocked(fetchProjects).mockImplementation(async () => ({ projects: mockProjects, default: 'p1' }))
})

describe('LiveProvider', () => {
  it('critério 1: troca via setActivePid reseta o hash para o dashboard', async () => {
    window.location.hash = '#/f/F-0001'
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    const btn = await screen.findByText('p1')
    await act(async () => {
      btn.click()
    })
    expect(window.location.hash).toBe('')
  })

  it('critério 3: deep link no load inicial não é redirecionado', async () => {
    window.location.hash = '#/f/F-0001'
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    expect(window.location.hash).toBe('#/f/F-0001')
  })
})

// F-0242: o `.catch` do bootstrap não pode fingir prontidão — senão as telas
// montam com pid vazio e disparam /api/projects//... (404 permanente).
describe('LiveProvider — falha no bootstrap (F-0242 critério 6)', () => {
  it('fetchProjects rejeitando mostra erro/retry e não libera os children', async () => {
    vi.mocked(fetchProjects).mockRejectedValueOnce(new Error('offline'))
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await waitFor(() => expect(screen.getByText(/erro ao carregar projetos: offline/i)).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: 'p1' })).not.toBeInTheDocument()

    vi.mocked(fetchProjects).mockResolvedValueOnce({ projects: mockProjects, default: 'p1' })
    await act(async () => {
      screen.getByRole('button', { name: /tentar de novo/i }).click()
    })
    await waitFor(() => expect(screen.getByText('p1')).toBeInTheDocument())
  })

  it('lista de projetos vazia mostra mensagem própria, não children quebrados', async () => {
    vi.mocked(fetchProjects).mockResolvedValueOnce({ projects: [], default: '' })
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await waitFor(() => expect(screen.getByText(/nenhum projeto registrado/i)).toBeInTheDocument())
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})

// F-0288: o projeto ativo é por ABA. sessionStorage guarda a escolha desta aba
// (sobrevive a reload); localStorage guarda só o default de uma aba nova. Uma
// aba trocando de projeto não pode arrastar as outras.
describe('LiveProvider — projeto ativo por aba (F-0288)', () => {
  it('critério 1: evento storage de outra aba não troca o projeto nem mexe no hash', async () => {
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    window.location.hash = '#/f/F-0001'
    await act(async () => {
      window.dispatchEvent(
        new StorageEvent('storage', { key: 'cerne.activePid', newValue: 'p2' }),
      )
    })
    expect(screen.getByText('p1')).toBeInTheDocument()
    expect(window.location.hash).toBe('#/f/F-0001')
  })

  it('critério 2: sessionStorage vence localStorage no boot (reload mantém a aba)', async () => {
    sessionStorage.setItem('cerne.activePid', 'p2')
    localStorage.setItem('cerne.activePid', 'p1')
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    expect(await screen.findByText('p2')).toBeInTheDocument()
  })

  it('critério 3: sem sessionStorage, a aba nova herda a última escolha (localStorage)', async () => {
    localStorage.setItem('cerne.activePid', 'p2')
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    expect(await screen.findByText('p2')).toBeInTheDocument()
  })

  it('setActivePid grava nas duas storages', async () => {
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    const btn = await screen.findByText('p1')
    await act(async () => {
      btn.click()
    })
    expect(sessionStorage.getItem('cerne.activePid')).toBe('p2')
    expect(localStorage.getItem('cerne.activePid')).toBe('p2')
  })

  it('critério 5: pid salvo que sumiu da lista cai no default do servidor', async () => {
    sessionStorage.setItem('cerne.activePid', 'p9')
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    expect(await screen.findByText('p1')).toBeInTheDocument()
  })
})
