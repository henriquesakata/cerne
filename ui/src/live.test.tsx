import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, act, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { LiveProvider, useProjects } from './live'
import { fetchProjects } from './api'
import type { ProjectInfo } from './api'

// F-0209: trocar de projeto (seletor ou sincronização entre abas) sempre
// aterrissa no dashboard — nenhuma rota de detalhe sobrevive à troca.

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

  it('critério 2: troca via evento storage (outra aba) reseta o hash', async () => {
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
